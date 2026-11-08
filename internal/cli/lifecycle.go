package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var (
		setFlag  string
		from     string
		harness  string
		project  string
		force    bool
		offline  bool
		noConfig bool
		jsonOut  bool
		frozen   bool
		update   bool
		except   []string
		only     bool
		latest   bool
	)
	c := newCmd("install <package> [for <harness>] [from <namespace>]", "Materialize a package or one part for a harness")
	c.Long = `Decodes a package into the native layout of the target harness. The natural form
is "also install <package> for <harness>" or
"also install <part> <kind> from <package> [for <harness>] [from <namespace>]".
Installs always target the current directory. Instructions are installed as a
managed block; skills, commands and agents as files; MCP and hooks are merged
into native config with reversible bookkeeping.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			if !frozen {
				return fmt.Errorf("usage: also install <package> [for <harness>] [from <namespace>], or also install --frozen with no package to replay also.lock")
			}
			return installFromLock(cfg, project, force, offline, jsonOut)
		}
		spec, err := parseInstallArgs(args, cfg)
		if err != nil {
			return err
		}
		spec.Exclude = splitSelectors(except)
		spec.OnlyParts = only
		proj, err := installProject(project)
		if err != nil {
			return err
		}
		requestedHarness := harness
		if spec.HarnessRequested {
			if requestedHarness != "" && !strings.EqualFold(requestedHarness, spec.Harness) {
				return fmt.Errorf("two different harnesses: %q and %q", requestedHarness, spec.Harness)
			}
			requestedHarness = spec.Harness
		}
		if requestedHarness == "" && !spec.HarnessRequested {
			// The project layout is evidence; a machine-wide default is not.
			if id, derr := also.DetectHarness(proj); derr == nil {
				requestedHarness = id
			} else if _, in, ferr := also.FindInfra(proj); ferr == nil && in.Harness != "" {
				requestedHarness = in.Harness
			}
		}
		harnessID, err := resolveHarnessWord(cfg, requestedHarness, spec.HarnessRequested)
		if err != nil {
			return err
		}
		installSet := setFlag
		if spec.Ref.Namespace != "" {
			if installSet != "" && installSet != spec.Ref.Namespace {
				return fmt.Errorf("namespace %q conflicts with --namespace %q", spec.Ref.Namespace, installSet)
			}
			if from != "" {
				return fmt.Errorf("namespace %q cannot be combined with --from <url>", spec.Ref.Namespace)
			}
			installSet = spec.Ref.Namespace
		}
		constraint := spec.Ref.Version
		lock, err := also.LoadLock(proj)
		if err != nil {
			return err
		}
		var entry *also.LockEntry
		if installSet != "" {
			entry = lock.FindIn(installSet, spec.Ref.Package, harnessID)
		} else {
			entry = lock.Find(spec.Ref.Package, harnessID)
			if entry != nil {
				for i := range lock.Packages {
					other := &lock.Packages[i]
					if other.Package == spec.Ref.Package && other.Harness == harnessID && other.Namespace != entry.Namespace {
						return fmt.Errorf("package %q is pinned in more than one namespace; pass from <namespace>", spec.Ref.Package)
					}
				}
			}
		}
		if frozen && entry == nil {
			return fmt.Errorf("no lockfile entry for %s in %s; run without --frozen to resolve it", spec.Ref.Package, proj)
		}
		if entry != nil {
			if err := entry.CheckEntry(); err != nil {
				return err
			}
			if frozen {
				if constraint != "" && constraint != entry.Version {
					return fmt.Errorf("lockfile pins %s@%s but you requested %s; update %s first", spec.Ref.Package, entry.Version, constraint, also.LockFile)
				}
				spec.Ref.Version = entry.Version
				if len(spec.Parts) == 0 {
					spec.Parts = entry.Parts
				}
				if !entry.WithConfig {
					noConfig = true
				}
			} else if constraint == "" {
				if update && entry.Constraint != "" {
					// --update re-resolves inside the recorded constraint.
					// A major change needs an explicit version.
					spec.Ref.Version = entry.Constraint
					constraint = entry.Constraint
				} else if !update {
					spec.Ref.Version = entry.Version
				}
			}
			if installSet == "" && from == "" {
				installSet = entry.Namespace
				cfg = configWithLockNamespace(cfg, entry.Namespace, entry.Remote)
				if _, ok := cfg.Namespaces[entry.Namespace]; !ok {
					return fmt.Errorf("lockfile namespace %q is not configured; run: also connect %s <git-url>", entry.Namespace, entry.Namespace)
				}
			} else if frozen {
				cfg = configWithLockNamespace(cfg, entry.Namespace, entry.Remote)
			}
		}
		expectedDigest := ""
		if frozen && entry != nil {
			expectedDigest = entry.Digest
		}
		return withLock(func() error {
			plan, setRepo, err := also.InstallPackage(cfg, also.InstallRequest{
				Package: spec.Ref.spec(), Harness: harnessID, Scope: "project", Project: proj,
				Parts: spec.Parts, Exclude: spec.Exclude, OnlyParts: spec.OnlyParts,
				Force: force, WithConfig: !noConfig, Offline: offline,
				ExpectedDigest: expectedDigest,
			}, installSet, from)
			if err != nil {
				return err
			}
			if frozen {
				if err := entry.VerifyInstall(&plan.Install); err != nil {
					return err
				}
			} else {
				lockConstraint := constraint
				if lockConstraint == "" && entry != nil {
					lockConstraint = entry.Constraint
				}
				if lockConstraint == "" && !latest {
					if sem, ok := setRepo.HighestSemverForVersion(plan.Install.Package, plan.Install.Version); ok {
						lockConstraint = "^" + sem.String()
					}
				}
				// Reload under the lock so a concurrent install in this project
				// does not lose its entry.
				if current, lerr := also.LoadLock(proj); lerr == nil {
					lock = current
				}
				lock.Upsert(also.LockEntryFromInstall(&plan.Install, lockConstraint))
				if err := also.SaveLock(proj, lock); err != nil {
					return err
				}
			}
			if jsonOut {
				return printJSON(planOutput(plan))
			}
			printPlan(plan)
			return nil
		})
	}
	addNamespaceFlag(c, &setFlag)
	c.Flags().StringVar(&from, "from", "", "use a remote git URL without configuring a namespace")
	c.Flags().StringVarP(&harness, "harness", "h", "", "target harness (claude, opencode, codex, qwen, ...)")
	c.Flags().StringVarP(&project, "dir", "p", "", "directory to install into (default: current directory)")
	c.Flags().BoolVar(&force, "force", false, "overwrite conflicting files and modified content (backups are kept)")
	c.Flags().BoolVar(&offline, "offline", false, "use the local clone without fetching")
	c.Flags().BoolVar(&noConfig, "no-config", false, "do not install MCP servers or hooks")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&frozen, "frozen", false, "install exactly what also.lock pins; fail on any difference")
	c.Flags().BoolVar(&update, "update", false, "ignore the lock pins and resolve the newest version")
	c.Flags().StringArrayVar(&except, "except", nil, "exclude a kind or name, e.g. --except instructions (repeatable)")
	c.Flags().BoolVar(&only, "only", false, "replace the existing part selection instead of adding to it")
	c.Flags().BoolVar(&latest, "latest", false, "do not pin the resolved semantic version to ^major.minor.patch")
	return c
}

func splitSelectors(in []string) []string {
	var out []string
	for _, item := range in {
		for _, part := range strings.Split(item, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return also.UniqueStrings(out)
}

type configChange struct {
	File   string   `json:"file"`
	Kind   string   `json:"kind"`
	Path   []string `json:"path,omitempty"`
	Marker string   `json:"marker,omitempty"`
}

type installOutput struct {
	Package       string         `json:"package"`
	Version       string         `json:"version"`
	Label         string         `json:"label,omitempty"`
	Harness       string         `json:"harness"`
	Project       string         `json:"dir,omitempty"`
	Root          string         `json:"root"`
	Namespace     string         `json:"namespace"`
	Parts         []string       `json:"parts,omitempty"`
	Exclude       []string       `json:"exclude,omitempty"`
	Files         int            `json:"files"`
	Instructions  string         `json:"instructions,omitempty"`
	ConfigChanges []configChange `json:"config_changes,omitempty"`
	Warnings      []string       `json:"warnings,omitempty"`
	Skipped       []string       `json:"skipped,omitempty"`
}

func planOutput(plan *also.Plan) installOutput {
	in := plan.Install
	out := installOutput{
		Package: in.Package, Version: in.Version, Label: in.Label,
		Harness: in.Harness, Project: in.Project, Root: in.Root,
		Namespace: in.Set, Parts: in.Parts, Exclude: in.Exclude, Files: len(plan.NewFiles),
		Warnings: unique(plan.Warnings), Skipped: unique(plan.Skipped),
	}
	if plan.Block != nil {
		out.Instructions = plan.Block.File
	}
	for _, e := range plan.Edits {
		out.ConfigChanges = append(out.ConfigChanges, configChange{File: e.File, Kind: e.Kind, Path: e.Path, Marker: e.Marker})
	}
	return out
}

// installFromLock replays every entry in also.lock. It is the CI path:
// one command, no package argument, no resolution.
// configWithLockNamespace lets a fresh machine (or CI) replay a lock without
// a separate `also setup`: the lock already records the remote.
func configWithLockNamespace(cfg *also.Config, name, remote string) *also.Config {
	if cfg == nil || name == "" || remote == "" {
		return cfg
	}
	if _, ok := cfg.Namespaces[name]; ok {
		return cfg
	}
	cp := *cfg
	cp.Namespaces = make(map[string]also.NamespaceConfig, len(cfg.Namespaces)+1)
	for k, v := range cfg.Namespaces {
		cp.Namespaces[k] = v
	}
	cp.Namespaces[name] = also.NamespaceConfig{Remote: remote}
	return &cp
}

// checkLockConflicts catches two entries that would write the same config
// path or destination differently. The apply loop mutates state between
// entries, so this must be checked before the first write.
func checkLockConflicts(plans []*also.Plan, force bool) error {
	_ = force
	edits := map[string]string{}
	files := map[string]string{}
	for _, p := range plans {
		for _, e := range p.Edits {
			if e.Kind == "json-append" {
				continue
			}
			key := e.File + "\x00" + strings.Join(e.Path, ".")
			value := string(e.Value) + "\x00" + e.Block + "\x00" + e.Marker
			if prev, ok := edits[key]; ok && prev != value {
				return fmt.Errorf("also.lock entries configure %s (%s) differently; use --force or remove one", e.File, strings.Join(e.Path, "."))
			}
			edits[key] = value
		}
		for _, f := range p.NewFiles {
			if prev, ok := files[f.Dest]; ok && prev != string(f.Data) {
				return fmt.Errorf("also.lock entries write %s differently; use --force or remove one", f.Dest)
			}
			files[f.Dest] = string(f.Data)
		}
	}
	return nil
}

func installFromLock(cfg *also.Config, dir string, force, offline, jsonOut bool) error {
	project, err := installProject(dir)
	if err != nil {
		return err
	}
	lock, err := also.LoadLock(project)
	if err != nil {
		return err
	}
	if len(lock.Packages) == 0 {
		return fmt.Errorf("also.lock is missing or empty in %s; run also install <package> first", project)
	}
	return withLock(func() error {
		lock, err = also.LoadLock(project)
		if err != nil {
			return err
		}
		// Preflight every entry before writing anything. A missing version,
		// digest mismatch or policy denial in entry N must not leave entries
		// 1..N-1 installed.
		policy, err := also.LoadVerifiedPolicy(project)
		if err != nil {
			return err
		}
		var plans []*also.Plan
		for _, entry := range lock.Packages {
			if err := entry.CheckEntry(); err != nil {
				return err
			}
			entryCfg := configWithLockNamespace(cfg, entry.Namespace, entry.Remote)
			if _, ok := entryCfg.Namespaces[entry.Namespace]; !ok {
				return fmt.Errorf("lockfile namespace %q is not configured and the lock has no remote", entry.Namespace)
			}
			set, err := also.ResolveSetForInstall(entryCfg, entry.Namespace, "", offline)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", entry.Namespace, entry.Package, err)
			}
			version, err := set.ResolveVersion(entry.Package, entry.Version)
			if err != nil {
				return fmt.Errorf("%s/%s@%s: %w", entry.Namespace, entry.Package, entry.Version, err)
			}
			pkg, err := set.LoadPackage(entry.Package, version.Version)
			if err != nil {
				return fmt.Errorf("%s/%s@%s: %w", entry.Namespace, entry.Package, entry.Version, err)
			}
			if entry.Digest != "" && pkg.Meta.Digest != entry.Digest {
				return fmt.Errorf("%s/%s: package digest %s does not match the lockfile digest %s", entry.Namespace, entry.Package, pkg.Meta.Digest, entry.Digest)
			}
			if err := also.CheckPolicyPackage(policy, entry.Namespace, pkg); err != nil {
				return fmt.Errorf("%s/%s: %w", entry.Namespace, entry.Package, err)
			}
			st, lerr := also.LoadState()
			if lerr != nil {
				return lerr
			}
			plan, perr := also.BuildPlan(st, set, pkg, version, entry.Harness, "project", project, also.InstallOptions{
				Force: force, WithConfig: entry.WithConfig, Parts: entry.Parts, Exclude: entry.Exclude,
			})
			if perr != nil {
				return fmt.Errorf("%s/%s: %w", entry.Namespace, entry.Package, perr)
			}
			if err := also.CheckPolicyExecutables(policy, plan.Install.Executables); err != nil {
				return fmt.Errorf("%s/%s: %w", entry.Namespace, entry.Package, err)
			}
			plans = append(plans, plan)
		}
		if err := checkLockConflicts(plans, force); err != nil {
			return err
		}
		beforeState, _ := also.LoadState()
		before := map[string]bool{}
		for _, in := range beforeState.Installs {
			before[in.ID] = true
		}
		type fileSnap struct {
			data []byte
			mode os.FileMode
		}
		type appliedEntry struct {
			id    string
			files map[string]fileSnap
		}
		var applied []appliedEntry
		rollback := func() {
			if len(applied) == 0 {
				return
			}
			st, lerr := also.LoadState()
			if lerr != nil {
				return
			}
			for _, item := range applied {
				if in := st.FindByID(item.id); in != nil {
					_, _ = also.Uninstall(st, in, true)
				}
				for path, snap := range item.files {
					_ = os.MkdirAll(filepath.Dir(path), 0o755)
					_ = os.WriteFile(path, snap.data, snap.mode)
				}
			}
		}
		out := make([]installOutput, 0, len(lock.Packages))
		for idx, entry := range lock.Packages {
			if err := entry.CheckEntry(); err != nil {
				return err
			}
			entryCfg := configWithLockNamespace(cfg, entry.Namespace, entry.Remote)
			if _, ok := entryCfg.Namespaces[entry.Namespace]; !ok {
				return fmt.Errorf("lockfile namespace %q is not configured and the lock has no remote; run: also connect %s <git-url>", entry.Namespace, entry.Namespace)
			}
			snap := map[string]fileSnap{}
			if idx < len(plans) {
				for _, f := range plans[idx].NewFiles {
					fi, serr := os.Stat(f.Dest)
					if serr != nil || !fi.Mode().IsRegular() {
						continue
					}
					if data, rerr := os.ReadFile(f.Dest); rerr == nil {
						snap[f.Dest] = fileSnap{data: data, mode: fi.Mode().Perm()}
					}
				}
			}
			plan, _, err := also.InstallPackage(entryCfg, also.InstallRequest{
				Package: entry.Package + "@" + entry.Version, Harness: entry.Harness,
				Scope: "project", Project: project, Parts: entry.Parts, Exclude: entry.Exclude, Force: force,
				WithConfig: entry.WithConfig, Offline: offline, ExpectedDigest: entry.Digest,
			}, entry.Namespace, "")
			if err != nil {
				rollback()
				return fmt.Errorf("%s/%s: %w", entry.Namespace, entry.Package, err)
			}
			if !before[plan.Install.ID] {
				applied = append(applied, appliedEntry{id: plan.Install.ID, files: snap})
			}
			out = append(out, planOutput(plan))
		}
		if jsonOut {
			return printJSON(out)
		}
		fmt.Printf("installed %d package(s) from also.lock\n", len(out))
		for _, item := range out {
			fmt.Printf("  %s/%s@%s -> %s\n", item.Namespace, item.Package, short(item.Version), item.Harness)
		}
		return nil
	})
}

func installProject(dir string) (string, error) {
	if dir != "" {
		return also.AbsPath(dir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return also.FindProjectRoot(cwd)
}

func resolveHarnessWord(cfg *also.Config, requested string, sawFor bool) (string, error) {
	if requested != "" {
		h, ok := also.ResolveHarness(requested)
		if !ok {
			return "", unknownHarness(requested)
		}
		return h.ID, nil
	}
	if sawFor {
		return "", clarifyHarnessFor(cfg)
	}
	id, err := cfg.Harness("")
	if err != nil {
		return "", clarifyHarness()
	}
	return id, nil
}

func printPlan(plan *also.Plan) {
	fmt.Printf("installed %s@%s", plan.Install.Package, short(plan.Install.Version))
	if plan.Install.Label != "" {
		fmt.Printf(" (%s)", plan.Install.Label)
	}
	fmt.Printf(" -> %s\n", plan.Install.Harness)
	fmt.Printf("root:  %s\n", plan.Install.Root)
	if len(plan.Install.Parts) > 0 {
		fmt.Printf("parts: %s\n", strings.Join(plan.Install.Parts, ", "))
	}
	fmt.Printf("files: %d\n", len(plan.NewFiles))
	if plan.Block != nil {
		fmt.Printf("instructions: %s\n", plan.Block.File)
	}
	if len(plan.Edits) > 0 {
		fmt.Printf("config changes: %d\n", len(plan.Edits))
		for _, e := range plan.Edits {
			what := e.Kind
			if len(e.Path) > 0 {
				what += " " + strings.Join(e.Path, ".")
			}
			if e.Marker != "" {
				what += " " + e.Marker
			}
			fmt.Printf("  - %s (%s)\n", e.File, what)
		}
	}
	printWarnings("skipped", plan.Skipped)
	printWarnings("warnings", plan.Warnings)
}

func newUpgradeCmd() *cobra.Command {
	var (
		check        bool
		force        bool
		jsonOut      bool
		legacyGlobal bool
		major        bool
	)
	c := newCmd("upgrade [package...]", "Upgrade packages installed in this directory")
	c.Long = "Upgrades packages installed in the current directory for the harness each one\nwas installed for. Content is content-addressed: 'up to date' means the installed\ndigest is still the newest."
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		cwd, err := installProject("")
		if err != nil {
			return err
		}
		var infra *also.Infra
		if _, in, ferr := also.FindInfra(cwd); ferr == nil {
			infra = in
		}
		pkgs := args
		if len(pkgs) == 0 && infra != nil && infra.Name != "" {
			pkgs = []string{infra.Name}
		}
		return withLock(func() error {
			st, err := also.LoadState()
			if err != nil {
				return err
			}
			results, err := also.UpgradeInstallsInProject(cfg, st, cwd, pkgs, force, check, legacyGlobal, major)
			if err != nil {
				return err
			}
			if !check {
				if err := refreshLockAfterUpgrade(cwd, results); err != nil {
					return err
				}
			}
			failed := 0
			if jsonOut {
				out := make([]upgradeOutput, 0, len(results))
				for _, r := range results {
					item := upgradeOutput{
						Package: r.Install.Package, Harness: r.Install.Harness,
						Old: r.Old, New: r.New, Status: r.Status,
					}
					if r.Err != nil {
						item.Status = "error"
						item.Error = r.Err.Error()
						failed++
					}
					out = append(out, item)
				}
				if err := printJSON(out); err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d upgrade(s) failed", failed)
				}
				return nil
			}
			w := newTabWriter()
			fmt.Fprintf(w, "PACKAGE\tHARNESS\tOLD\tNEW\tSTATUS\n")
			for _, r := range results {
				status := r.Status
				if r.Err != nil {
					status = "error: " + r.Err.Error()
					failed++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Install.Package, r.Install.Harness, short(r.Old), short(r.New), status)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("%d upgrade(s) failed; nothing was rolled back for successful ones", failed)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&check, "check", false, "only report available updates")
	c.Flags().BoolVar(&force, "force", false, "overwrite locally modified files")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&major, "major", false, "allow an upgrade to a new major version")
	c.Flags().BoolVar(&legacyGlobal, "legacy-global", false, "report old global installs")
	_ = c.Flags().MarkHidden("legacy-global")
	return c
}

// refreshLockAfterUpgrade pins the versions that actually landed on disk.
func refreshLockAfterUpgrade(project string, results []also.UpgradeResult) error {
	lock, err := also.LoadLock(project)
	if err != nil {
		return err
	}
	st, err := also.LoadState()
	if err != nil {
		return err
	}
	changed := false
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		for i := range st.Installs {
			in := &st.Installs[i]
			if in.Package != r.Install.Package || in.Harness != r.Install.Harness {
				continue
			}
			if also.PathKey(in.Project) != also.PathKey(project) {
				continue
			}
			constraint := r.Constraint
			if constraint == "" {
				if old := lock.FindIn(in.Set, in.Package, in.Harness); old != nil {
					constraint = old.Constraint
				}
			}
			lock.Upsert(also.LockEntryFromInstall(in, constraint))
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return also.SaveLock(project, lock)
}

// refreshLockAfterReshape records the harness change. Without this, a
// reshaped project audits as lock-extra/lock-not-installed and CI replays the
// old harness.
func refreshLockAfterReshape(results []also.ReshapeResult) error {
	st, err := also.LoadState()
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.Err != nil || r.Status != "reshaped" || r.Install.Project == "" {
			continue
		}
		project := r.Install.Project
		if _, err := os.Stat(also.LockfilePath(project)); os.IsNotExist(err) {
			continue
		}
		lock, err := also.LoadLock(project)
		if err != nil {
			return err
		}
		var next *also.Install
		for i := range st.Installs {
			in := &st.Installs[i]
			if in.Package == r.Install.Package && in.Harness == r.To &&
				also.PathKey(in.Project) == also.PathKey(project) &&
				in.Set == r.Install.Set && in.Scope == r.Install.Scope {
				next = in
				break
			}
		}
		if next == nil {
			continue
		}
		constraint := ""
		if old := lock.FindIn(r.Install.Set, r.Install.Package, r.Install.Harness); old != nil {
			constraint = old.Constraint
			lock.RemoveIn(r.Install.Set, r.Install.Package, r.Install.Harness)
		}
		if next.Semver != "" {
			if v, ok := also.ParseSemver(next.Semver); ok {
				if rng, ok := also.ParseSemverRange(constraint); constraint != "" && (!ok || !rng.Match(v)) {
					constraint = "^" + v.String()
				} else if constraint == "" {
					constraint = "^" + v.String()
				}
			}
		}
		lock.Upsert(also.LockEntryFromInstall(next, constraint))
		if err := also.SaveLock(project, lock); err != nil {
			return err
		}
	}
	return nil
}

func preflightIgnoreUpdate(project string) error {
	path := filepath.Join(project, ".gitignore")
	if root, err := also.GitProjectRoot(project); err == nil {
		path = filepath.Join(root, ".gitignore")
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to reshape: %s is a symlink; make it a regular file first", path)
	}
	return nil
}

// updateIgnoreAfterReshape extends the managed infra block for the new
// harness. Without this, an agent's "git add -A" stages .claude/CLAUDE.md and
// the next audit fails on tracked infrastructure.
func updateIgnoreAfterReshape(results []also.ReshapeResult) error {
	st, err := also.LoadState()
	if err != nil {
		return err
	}
	// The managed block belongs to the git root and is shared by every project
	// under it. A reshape of one project must union the harnesses, never
	// replace the block with only the new harness.
	harnesses := map[string]map[string]bool{}
	pathByKey := map[string]string{}
	gitByKey := map[string]bool{}
	add := func(project, harness string) {
		if project == "" || harness == "" {
			return
		}
		root, err := also.GitProjectRoot(project)
		key, path, git := project, project, false
		if err == nil {
			key, path, git = root, root, true
		}
		if harnesses[key] == nil {
			harnesses[key] = map[string]bool{}
			pathByKey[key] = path
			gitByKey[key] = git
		}
		harnesses[key][harness] = true
	}
	for _, r := range results {
		if r.Err != nil || r.Status != "reshaped" || r.Install.Project == "" {
			continue
		}
		add(r.Install.Project, r.Install.Harness)
		add(r.Install.Project, r.To)
	}
	for key, path := range pathByKey {
		for _, in := range st.Installs {
			if in.Harness == "" || in.Project == "" {
				continue
			}
			if gitByKey[key] {
				rel, rerr := filepath.Rel(path, in.Project)
				if rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					harnesses[key][in.Harness] = true
				}
			} else if also.PathKey(in.Project) == also.PathKey(path) {
				harnesses[key][in.Harness] = true
			}
		}
	}
	for key, list := range harnesses {
		path := pathByKey[key]
		gitignore := filepath.Join(path, ".gitignore")
		data, err := os.ReadFile(gitignore)
		if err != nil || !strings.Contains(string(data), "# also:begin infra") {
			continue
		}
		var names []string
		for h := range list {
			names = append(names, h)
		}
		sort.Strings(names)
		entries := also.InfraIgnoreEntries(names)
		if gitByKey[key] {
			if _, err := also.UpdateGitIgnore(path, also.IgnoreInfra, true, entries); err != nil {
				return err
			}
		} else if _, err := also.UpdateGitIgnoreAt(gitignore, also.IgnoreInfra, true, entries); err != nil {
			return err
		}
		for _, in := range st.Installs {
			match := false
			if gitByKey[key] {
				rel, rerr := filepath.Rel(path, in.Project)
				match = rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
			} else {
				match = also.PathKey(in.Project) == also.PathKey(path)
			}
			if !match {
				continue
			}
			if _, err := os.Stat(filepath.Join(in.Project, ".also-layers")); err == nil {
				if gitByKey[key] {
					if _, err := also.UpdateGitIgnore(path, also.IgnoreAlso, true, also.AlsoIgnoreEntries()); err != nil {
						return err
					}
				} else if _, err := also.UpdateGitIgnoreAt(gitignore, also.IgnoreAlso, true, also.AlsoIgnoreEntries()); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

type upgradeOutput struct {
	Package string `json:"package"`
	Harness string `json:"harness"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

func newReshapeCmd() *cobra.Command {
	var (
		force   bool
		jsonOut bool
		project string
		all     bool
	)
	c := newCmd("reshape <harness> [package...]", "Re-materialize installed packages for another harness")
	c.Long = `Transforms installed packages in this project into the layout of another
harness, keeping the installed version, and updates also.lock so CI can replay
the new harness. All packages are preflighted before the first write; each
package is applied transactionally. Use --all only when you really mean every
project on this machine.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("usage: also reshape <harness> [package...]")
		}
		target, ok := also.ResolveHarness(args[0])
		if !ok {
			return fmt.Errorf("unknown harness %q; available: %v", args[0], also.HarnessIDs())
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		proj := ""
		if !all {
			proj, err = installProject(project)
			if err != nil {
				return err
			}
		}
		if all {
			pre, err := also.LoadState()
			if err != nil {
				return err
			}
			for _, in := range pre.Installs {
				if in.Project != "" {
					if err := preflightIgnoreUpdate(in.Project); err != nil {
						return err
					}
				}
			}
		} else if err := preflightIgnoreUpdate(proj); err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		return withLock(func() error {
			// Reload under the lock so a concurrent command cannot move state
			// between the read and the reshape.
			st, err = also.LoadState()
			if err != nil {
				return err
			}
			results, err := also.Reshape(cfg, st, target.ID, proj, all, args[1:], force)
			if err != nil {
				return err
			}
			if err := refreshLockAfterReshape(results); err != nil {
				return err
			}
			if err := updateIgnoreAfterReshape(results); err != nil {
				return err
			}
			failed := 0
			if jsonOut {
				out := make([]reshapeOutput, 0, len(results))
				for _, r := range results {
					item := reshapeOutput{
						Package: r.Install.Package, From: r.Install.Harness,
						To: r.To, Status: r.Status, Warnings: unique(r.Warnings),
					}
					if r.Err != nil {
						item.Status = "error"
						item.Error = r.Err.Error()
						failed++
					}
					out = append(out, item)
				}
				if err := printJSON(out); err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d reshape(s) failed", failed)
				}
				return nil
			}
			w := newTabWriter()
			fmt.Fprintf(w, "PACKAGE\tFROM\tTO\tSTATUS\n")
			for _, r := range results {
				status := r.Status
				if r.Err != nil {
					status = "error: " + r.Err.Error()
					failed++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Install.Package, r.Install.Harness, r.To, status)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			for _, r := range results {
				if len(r.Warnings) > 0 {
					fmt.Printf("  %s warnings:\n", r.Install.Package)
					for _, warning := range unique(r.Warnings) {
						fmt.Printf("    - %s\n", warning)
					}
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d reshape(s) failed", failed)
			}
			return nil
		})
	}
	c.Flags().StringVarP(&project, "dir", "p", "", "project to reshape (default: nearest also.lock/git root)")
	c.Flags().BoolVar(&all, "all", false, "reshape every project on this machine")
	c.Flags().BoolVar(&force, "force", false, "overwrite locally modified files")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

type reshapeOutput struct {
	Package  string   `json:"package"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Status   string   `json:"status"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func newTreeCmd() *cobra.Command {
	var (
		jsonOut bool
		harness string
	)
	c := newCmd("tree", "Show only the installed AI infrastructure")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		args, forHarness, err := parseTrailingFor(args)
		if err != nil {
			return err
		}
		if len(args) > 0 {
			return fmt.Errorf("usage: also tree [for <harness>]")
		}
		if forHarness != "" {
			if harness != "" && !strings.EqualFold(harness, forHarness) {
				return fmt.Errorf("two different harnesses: %q and %q", harness, forHarness)
			}
			harness = forHarness
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		report := also.BuildTree(st)
		if harness != "" {
			h, ok := also.ResolveHarness(harness)
			if !ok {
				return fmt.Errorf("unknown harness %q; available: %v", harness, also.HarnessIDs())
			}
			var filtered []also.TreeInstall
			for _, t := range report {
				if t.Install.Harness == h.ID {
					filtered = append(filtered, t)
				}
			}
			report = filtered
		}
		if jsonOut {
			return printJSON(report)
		}
		if len(report) == 0 {
			fmt.Println("no installed infrastructure")
			return nil
		}
		sort.Slice(report, func(i, j int) bool {
			a, b := report[i].Install, report[j].Install
			if a.Harness != b.Harness {
				return a.Harness < b.Harness
			}
			if a.Root != b.Root {
				return a.Root < b.Root
			}
			return a.Package < b.Package
		})
		lastGroup := ""
		for _, t := range report {
			in := t.Install
			group := in.Harness + " " + in.Root
			if group != lastGroup {
				fmt.Printf("\n%s  [%s]\n", orDash(in.Root), in.Harness)
				lastGroup = group
			}
			fmt.Printf("  %s/%s@%s", in.Set, in.Package, short(in.Version))
			if in.Label != "" {
				fmt.Printf("  (%s)", in.Label)
			}
			fmt.Printf("  %s  installed %s\n", in.Harness, also.InstalledAt(in))
			for _, f := range t.Files {
				mark := ""
				if !f.Exists {
					mark = "  [missing]"
				} else if f.Modified {
					mark = "  [modified]"
				}
				rel := f.Path
				if r, err := filepath.Rel(in.Root, f.Path); err == nil && !strings.HasPrefix(r, "..") {
					rel = r
				}
				fmt.Printf("    %s%s\n", rel, mark)
			}
			if in.Block != nil {
				fmt.Printf("    (instructions block: %s)\n", in.Block.File)
			}
			if len(in.Edits) > 0 {
				fmt.Printf("    (config entries: %d)\n", len(in.Edits))
			}
			if len(in.Skipped) > 0 {
				fmt.Printf("    (skipped: %s)\n", strings.Join(in.Skipped, ", "))
			}
		}
		return nil
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().StringVar(&harness, "harness", "", "filter by harness")
	return c
}

func newUninstallCmd() *cobra.Command {
	var (
		force        bool
		all          bool
		harness      string
		namespace    string
		project      string
		jsonOut      bool
		legacyGlobal bool
	)
	c := newCmd("uninstall <package>", "Remove an installed package and its reversible config edits")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		args, forHarness, err := parseTrailingFor(args)
		if err != nil {
			return err
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: also uninstall <package> [for <harness>]")
		}
		if forHarness != "" {
			if harness != "" && !strings.EqualFold(harness, forHarness) {
				return fmt.Errorf("two different harnesses: %q and %q", harness, forHarness)
			}
			harness = forHarness
		}
		proj := project
		if proj == "" {
			proj, err = installProject("")
			if err != nil {
				return err
			}
		}
		absProj, err := also.AbsPath(proj)
		if err != nil {
			return err
		}
		return withLock(func() error {
			st, err := also.LoadState()
			if err != nil {
				return err
			}
			var matches []*also.Install
			for _, in := range st.InstallsOf(args[0]) {
				if legacyGlobal {
					if in.Scope != "global" {
						continue
					}
				} else {
					if in.Scope == "global" || also.PathKey(in.Project) != also.PathKey(absProj) {
						continue
					}
				}
				if harness != "" {
					h, ok := also.ResolveHarness(harness)
					if !ok || in.Harness != h.ID {
						continue
					}
				}
				if namespace != "" && in.Set != namespace {
					continue
				}
				matches = append(matches, in)
			}
			if len(matches) == 0 {
				if legacyGlobal {
					return fmt.Errorf("package %q has no legacy global install", args[0])
				}
				return fmt.Errorf("package %q is not installed in %s", args[0], absProj)
			}
			if len(matches) > 1 && !all {
				var lines []string
				for _, in := range matches {
					lines = append(lines, fmt.Sprintf("  %s (%s)", in.Harness, in.Root))
				}
				return fmt.Errorf("package %q is installed %d times; add --all or filter with --harness/--namespace:\n%s", args[0], len(matches), strings.Join(lines, "\n"))
			}
			ids := make([]string, 0, len(matches))
			for _, in := range matches {
				ids = append(ids, in.ID)
			}
			type uninstallOutput struct {
				Namespace string   `json:"namespace"`
				Package   string   `json:"package"`
				Version   string   `json:"version"`
				Harness   string   `json:"harness"`
				Warnings  []string `json:"warnings,omitempty"`
			}
			var out []uninstallOutput
			for _, id := range ids {
				in := st.FindByID(id)
				if in == nil {
					continue
				}
				// Uninstall removes the slice element, so read everything the
				// output and lockfile need before the mutation.
				namespace, pkg, version, harness := in.Set, in.Package, in.Version, in.Harness
				warnings, err := also.Uninstall(st, in, force)
				if err != nil {
					return err
				}
				item := uninstallOutput{
					Namespace: namespace, Package: pkg, Version: version,
					Harness: harness, Warnings: unique(warnings),
				}
				out = append(out, item)
				if !jsonOut {
					fmt.Printf("uninstalled %s/%s@%s from %s\n", namespace, pkg, short(version), harness)
					printWarnings("warnings", warnings)
				}
			}
			if !legacyGlobal {
				lock, err := also.LoadLock(absProj)
				if err != nil {
					return err
				}
				if len(lock.Packages) > 0 {
					for _, item := range out {
						lock.RemoveIn(item.Namespace, item.Package, item.Harness)
					}
					if err := also.SaveLock(absProj, lock); err != nil {
						return err
					}
				}
			}
			if jsonOut {
				return printJSON(out)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&force, "force", false, "remove locally modified files too")
	c.Flags().BoolVar(&all, "all", false, "remove every matching installation")
	c.Flags().StringVar(&harness, "harness", "", "filter by harness")
	c.Flags().StringVar(&namespace, "namespace", "", "filter by namespace")
	c.Flags().StringVarP(&project, "dir", "p", "", "directory to uninstall from (default: current directory)")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&legacyGlobal, "legacy-global", false, "remove an old global install")
	_ = c.Flags().MarkHidden("legacy-global")
	return c
}

func newPruneCmd() *cobra.Command {
	var (
		dryRun  bool
		jsonOut bool
	)
	c := newCmd("prune", "Drop install records whose project directory no longer exists")
	c.Long = `Removes stale state entries after a project directory was deleted manually.
Only records whose project path is gone are removed; existing projects with
missing or modified files are left for audit and drift to report.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also prune [--dry-run] [--json]")
		}
		return withLock(func() error {
			st, err := also.LoadState()
			if err != nil {
				return err
			}
			type pruneOutput struct {
				Namespace string `json:"namespace"`
				Package   string `json:"package"`
				Version   string `json:"version"`
				Harness   string `json:"harness"`
				Project   string `json:"project"`
			}
			var out []pruneOutput
			for _, in := range st.PruneMissingProjects() {
				out = append(out, pruneOutput{
					Namespace: in.Set, Package: in.Package, Version: in.Version,
					Harness: in.Harness, Project: in.Project,
				})
			}
			if !dryRun && len(out) > 0 {
				if err := also.SaveState(st); err != nil {
					return err
				}
			}
			if jsonOut {
				return printJSON(map[string]any{"dry_run": dryRun, "removed": out})
			}
			if len(out) == 0 {
				fmt.Println("nothing to prune")
				return nil
			}
			for _, item := range out {
				verb := "pruned"
				if dryRun {
					verb = "would prune"
				}
				fmt.Printf("%s %s/%s@%s from %s (%s)\n", verb, item.Namespace, item.Package, short(item.Version), item.Harness, item.Project)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func newAdoptCmd() *cobra.Command {
	var (
		name        string
		harness     string
		into        string
		setFlag     string
		noPush      bool
		force       bool
		message     string
		desc        string
		allowSecret bool
		allowLarge  bool
		semver      string
		bump        string
		jsonOut     bool
	)
	c := newCmd("adopt [directory] [for <harness>]", "Capture the initialized infrastructure into a package")
	c.Long = `Extracts instructions, skills, commands, agents, plugins, hook scripts and native
config for one harness and pushes a new package version to the remote namespace.
The package name comes from also.json, so run "also init" first. The bundle is
temporary unless --into is given.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		args, forHarness, err := parseTrailingFor(args)
		if err != nil {
			return err
		}
		if len(args) > 1 {
			return fmt.Errorf("usage: also adopt [directory] [for <harness>]")
		}
		if forHarness != "" {
			if harness != "" && !strings.EqualFold(harness, forHarness) {
				return fmt.Errorf("two different harnesses: %q and %q", harness, forHarness)
			}
			harness = forHarness
		}
		target := "."
		if len(args) == 1 {
			target = args[0]
		}
		proj, infra, err := also.FindInfra(target)
		if err != nil {
			return err
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if name == "" {
			name = infra.Name
		}
		if harness == "" {
			harness = infra.Harness
		}
		harnessID := ""
		if harness != "" {
			h, ok := also.ResolveHarness(harness)
			if !ok {
				return fmt.Errorf("unknown harness %q; available: %v", harness, also.HarnessIDs())
			}
			harnessID = h.ID
		} else {
			harnessID, err = also.DetectHarness(proj)
			if err != nil {
				return err
			}
		}
		var setName string
		var sc also.NamespaceConfig
		if !noPush {
			setName, sc, err = requireSet(cmd, cfg, setFlag, "")
			if err != nil {
				return err
			}
		}
		if noPush && into == "" {
			return fmt.Errorf("--no-push needs --into <dir> so the bundle is kept")
		}
		dest := into
		if dest == "" {
			dest, err = os.MkdirTemp("", "also-adopt-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dest)
		}
		if !also.ValidName(name) {
			return fmt.Errorf("invalid package name %q; pass --name", name)
		}
		return withLock(func() error {
			res, err := also.ExtractBundle(also.AdoptOptions{
				Name: name, Project: proj, Harness: harnessID, Dest: dest,
				Force: force, AllowSecrets: allowSecret, AllowLarge: allowLarge,
			})
			if err != nil {
				return err
			}
			if !jsonOut {
				if noPush || into != "" {
					fmt.Printf("captured %s -> %s\n", harnessID, res.BundleDir)
				} else {
					fmt.Printf("captured %s\n", harnessID)
				}
				fmt.Printf("files: %d\n", res.Files)
				printWarnings("warnings", res.Warnings)
			}
			if noPush {
				if jsonOut {
					out := map[string]any{"files": res.Files, "warnings": unique(res.Warnings)}
					if into != "" {
						out["bundle"] = res.BundleDir
					}
					return printJSON(out)
				}
				return nil
			}
			repo, err := also.OpenSet(setName, sc, false)
			if err != nil {
				return err
			}
			pushRes, err := repo.Push(res.BundleDir, also.PushOptions{
				Name: name, Description: desc, Harness: harnessID, Message: message,
				AllowSecrets: allowSecret, AllowLarge: allowLarge, AllFiles: true,
				Semver: semver, Bump: bump,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				out := map[string]any{"files": res.Files, "warnings": unique(res.Warnings), "push": pushRes}
				if into != "" {
					out["bundle"] = res.BundleDir
				}
				return printJSON(out)
			}
			if pushRes.Existing {
				fmt.Printf("no changes: %s@%s already exists\n", name, short(pushRes.Version))
				return nil
			}
			fmt.Printf("pushed %s@%s [%s]\n", name, pushRes.Version, harnessID)
			if pushRes.Semver != "" {
				fmt.Printf("semver: %s\n", pushRes.Semver)
			}
			printWarnings("package warnings", pushRes.Warnings)
			return nil
		})
	}
	c.Flags().StringVar(&name, "name", "", "package name (default: from also.json)")
	c.Flags().StringVarP(&harness, "harness", "h", "", "source harness (default: from also.json)")
	c.Flags().StringVar(&into, "into", "", "keep the extracted bundle in this directory (debugging)")
	addNamespaceFlag(c, &setFlag)
	c.Flags().BoolVar(&noPush, "no-push", false, "only write the local bundle")
	c.Flags().BoolVar(&force, "force", false, "replace an existing bundle (kept as a backup)")
	c.Flags().StringVarP(&message, "message", "m", "", "git commit message")
	c.Flags().StringVar(&desc, "description", "", "package description")
	c.Flags().BoolVar(&allowSecret, "allow-secrets", false, "allow likely secrets")
	c.Flags().BoolVar(&allowLarge, "allow-large", false, "allow files larger than 8 MiB")
	c.Flags().StringVar(&semver, "semver", "", "tag the adopted version with an exact semantic version")
	c.Flags().StringVar(&bump, "bump", "", "tag the adopted version as the next major, minor or patch release")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}
