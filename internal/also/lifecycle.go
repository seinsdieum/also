package also

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ParsePkgSpec(spec string) (pkg, version string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("package name is empty")
	}
	if i := strings.LastIndex(spec, "@"); i >= 0 {
		pkg, version = spec[:i], spec[i+1:]
	} else {
		pkg = spec
	}
	if !ValidName(pkg) {
		return "", "", fmt.Errorf("invalid package name %q; use lowercase letters, digits, '.', '_' or '-'", pkg)
	}
	if version != "" && strings.ContainsAny(version, `/\`) {
		return "", "", fmt.Errorf("invalid version %q", version)
	}
	return pkg, version, nil
}

type InstallRequest struct {
	Package        string
	Harness        string
	Scope          string
	Project        string
	Parts          []string
	Exclude        []string
	OnlyParts      bool
	Force          bool
	WithConfig     bool
	Offline        bool
	ExpectedDigest string
}

func ResolveSetForInstall(cfg *Config, setName, remote string, offline bool) (*SetRepo, error) {
	if remote != "" {
		return openRemote(remote, !offline, offline)
	}
	name, sc, err := cfg.ResolveSet(setName)
	if err != nil {
		return nil, err
	}
	return openSet(name, sc, !offline, offline)
}

func InstallPackage(cfg *Config, req InstallRequest, setName, remote string) (*Plan, *SetRepo, error) {
	pkgName, verSpec, err := ParsePkgSpec(req.Package)
	if err != nil {
		return nil, nil, err
	}
	set, err := ResolveSetForInstall(cfg, setName, remote, req.Offline)
	if err != nil {
		return nil, nil, err
	}
	version, err := set.ResolveVersion(pkgName, verSpec)
	if err != nil {
		return nil, set, err
	}
	pkg, err := set.LoadPackage(pkgName, version.Version)
	if err != nil {
		return nil, set, err
	}
	if req.ExpectedDigest != "" && pkg.Meta.Digest != req.ExpectedDigest {
		return nil, set, fmt.Errorf("package %s@%s digest %s does not match the lockfile digest %s",
			pkgName, version.Version, pkg.Meta.Digest, req.ExpectedDigest)
	}
	scope, err := cfg.Scope(req.Scope)
	if err != nil {
		return nil, set, err
	}
	project := req.Project
	if project == "" {
		project, err = os.Getwd()
		if err != nil {
			return nil, set, err
		}
	}
	policy, err := LoadVerifiedPolicy(project)
	if err != nil {
		return nil, set, err
	}
	if err := checkPolicyPackage(policy, set.Name, pkg); err != nil {
		return nil, set, err
	}
	st, err := LoadState()
	if err != nil {
		return nil, set, err
	}
	PrepareKnownConfigOrigins(st, project)
	opts := InstallOptions{Force: req.Force, WithConfig: req.WithConfig, Parts: req.Parts,
		Exclude: req.Exclude, OnlyParts: req.OnlyParts}
	plan, err := BuildPlan(st, set, pkg, version, req.Harness, scope, project, opts)
	if err != nil {
		return nil, set, err
	}
	if err := checkPolicyExecutables(policy, plan.Install.Executables); err != nil {
		return nil, set, err
	}
	if !planHasChanges(plan) {
		if len(req.Parts) > 0 {
			var available []string
			if source, ok := ResolveHarness(pkg.Meta.SourceHarness); ok {
				available = AvailableParts(pkg, source)
			}
			return nil, set, fmt.Errorf("no installable changes for the requested parts in %s: %s\navailable parts: %s",
				pkg.Meta.Name, strings.Join(req.Parts, ", "), strings.Join(available, ", "))
		}
		return nil, set, fmt.Errorf("package %s has no components installable for %s", pkg.Meta.Name, req.Harness)
	}
	if err := ApplyConfigOrigins(st, project, plan.Edits, plan.Block); err != nil {
		return nil, set, err
	}
	if err := ApplyPlan(st, plan, opts); err != nil {
		return nil, set, err
	}
	if err := MergeConfigOrigins(project, plan.Edits, plan.Block); err != nil {
		return nil, set, err
	}
	return plan, set, nil
}

// CheckPolicyPackage is the exported preflight form used by frozen replay.
func CheckPolicyPackage(p *Policy, namespace string, pkg *Package) error {
	return checkPolicyPackage(p, namespace, pkg)
}

func checkPolicyPackage(p *Policy, namespace string, pkg *Package) error {
	if p == nil {
		return nil
	}
	if !p.NamespaceAllowed(namespace) {
		return fmt.Errorf("policy: namespace %q is not allowed; edit %s", namespace, PolicyFile)
	}
	if !p.PackageAllowed(pkg.Meta.Name) {
		return fmt.Errorf("policy: package %q is not allowed; edit %s", pkg.Meta.Name, PolicyFile)
	}
	source, ok := ResolveHarness(pkg.Meta.SourceHarness)
	if !ok {
		return fmt.Errorf("policy: unknown source harness %q", pkg.Meta.SourceHarness)
	}
	servers, _, _ := ExtractBundleMCP(source, pkg.Files, source.BundleRootsFromFiles(pkg.Files))
	if findings := p.CheckPackageFiles(pkg.Files, servers); len(findings) > 0 {
		return fmt.Errorf("policy: %s", strings.Join(findings, "; "))
	}
	return nil
}

// CheckPolicyExecutables is the exported preflight form used by frozen replay.
func CheckPolicyExecutables(p *Policy, execs []Executable) error {
	return checkPolicyExecutables(p, execs)
}

func checkPolicyExecutables(p *Policy, execs []Executable) error {
	if p == nil || len(execs) == 0 {
		return nil
	}
	var kinds []string
	for _, e := range execs {
		kinds = append(kinds, e.Kind)
	}
	if !p.ExecutablesAllowed(kinds) {
		return fmt.Errorf("policy: installed components include a disallowed executable kind (%s); edit %s",
			strings.Join(UniqueStrings(kinds), ", "), PolicyFile)
	}
	return nil
}

func planHasChanges(plan *Plan) bool {
	return len(plan.NewFiles) > 0 || plan.Block != nil || len(plan.Edits) > 0
}

type UpgradeResult struct {
	Install Install
	Old     string
	New     string
	// Constraint is the lock constraint that follows from this upgrade. It is
	// set when a major move requires replacing an old ^1.x pin.
	Constraint string
	Status     string
	Err        error
}

func UpgradeInstalls(cfg *Config, st *State, pkgs []string, force, check bool, major ...bool) ([]UpgradeResult, error) {
	installs := filterInstalls(st.InstallsOf(""), pkgs)
	if len(installs) == 0 {
		return nil, fmt.Errorf("no matching installs")
	}
	return upgradeInstalls(cfg, st, installs, force, check, len(major) > 0 && major[0])
}

// UpgradeInstallsInProject upgrades only the packages installed in one
// directory, which is what the "also upgrade" command targets. Legacy global
// installs are reported, not rewritten: their files live in the home dir.
func UpgradeInstallsInProject(cfg *Config, st *State, project string, pkgs []string, force, check, includeGlobal bool, major ...bool) ([]UpgradeResult, error) {
	abs, err := AbsPath(project)
	if err != nil {
		return nil, err
	}
	var installs []Install
	var legacy []UpgradeResult
	for _, in := range filterInstalls(st.InstallsOf(""), pkgs) {
		if in.Scope == "global" {
			if includeGlobal {
				legacy = append(legacy, UpgradeResult{
					Install: in, Old: in.Version, New: in.Version,
					Status: "legacy global; remove it with: also uninstall " + in.Package + " --legacy-global",
				})
			}
			continue
		}
		if PathKey(in.Project) == PathKey(abs) {
			installs = append(installs, in)
		}
	}
	if len(installs) == 0 && len(legacy) == 0 {
		return nil, fmt.Errorf("no packages installed in %s; run: also install <package> for <harness>", abs)
	}
	results, err := upgradeInstalls(cfg, st, installs, force, check, len(major) > 0 && major[0])
	if err != nil {
		return nil, err
	}
	return append(results, legacy...), nil
}

func upgradeInstalls(cfg *Config, st *State, installs []Install, force, check, major bool) ([]UpgradeResult, error) {
	repos := map[string]*SetRepo{}
	type pending struct {
		idx        int
		in         Install
		repo       *SetRepo
		latest     VersionInfo
		constraint string
	}
	var todo []pending
	out := make([]UpgradeResult, len(installs))
	// Preflight every package before writing anything. A policy denial or
	// resolution error on package N must not leave packages 1..N-1 upgraded.
	for i, in := range installs {
		res := UpgradeResult{Install: in, Old: in.Version}
		out[i] = res
		repo, err := upgradeRepo(cfg, &in, repos)
		if err != nil {
			out[i].Err = err
			continue
		}
		latest, err := repo.Latest(in.Package)
		constraint := ""
		if lock, lerr := LoadLock(in.Project); lerr == nil {
			if entry := lock.FindIn(in.Set, in.Package, in.Harness); entry != nil {
				constraint = entry.Constraint
			}
		}
		if constraint != "" && !major {
			latest, err = repo.ResolveVersion(in.Package, constraint)
		} else if !major {
			if sem, ok := repo.HighestSemverForVersion(in.Package, in.Version); ok {
				latest, err = repo.ResolveVersion(in.Package, "^"+sem.String())
			}
		}
		if err != nil {
			out[i].Err = err
			continue
		}
		out[i].Constraint = constraint
		if latestSem, ok := HighestSemverString(latest.Semver); ok {
			if constraint == "" {
				out[i].Constraint = "^" + latestSem.String()
			} else if rng, ok := ParseSemverRange(constraint); !ok || !rng.Match(latestSem) {
				out[i].Constraint = "^" + latestSem.String()
			}
			if installedSem, ok := repo.HighestSemverForVersion(in.Package, in.Version); ok &&
				CompareSemver(latestSem, installedSem) < 0 {
				out[i].Err = fmt.Errorf("refusing to downgrade %s from %s to %s; run also upgrade --major to choose a newer major", in.Package, installedSem, latestSem)
				continue
			}
		}
		out[i].New = latest.Version
		if latest.Version == in.Version {
			// "up to date" is misleading when the lock constraint hides a
			// newer release. Say what is actually available.
			if !major {
				if newest, nerr := repo.Latest(in.Package); nerr == nil && newest.Version != in.Version {
					if newSem, ok := HighestSemverString(newest.Semver); ok {
						if oldSem, ok := repo.HighestSemverForVersion(in.Package, in.Version); ok &&
							CompareSemver(newSem, oldSem) > 0 {
							out[i].New = newest.Version
							out[i].Status = "update available; locked constraint blocks it"
							// Before 1.0, a 0.x minor is the breaking gate too.
							if newSem.Major > oldSem.Major || (oldSem.Major == 0 && newSem.Minor > oldSem.Minor) {
								out[i].Status = "major update available; use --major"
							}
							continue
						}
					}
				}
			}
			out[i].Status = "up to date"
			continue
		}
		if check {
			out[i].Status = "update available"
			continue
		}
		pkg, err := repo.LoadPackage(in.Package, latest.Version)
		if err != nil {
			out[i].Err = err
			continue
		}
		policy, err := LoadVerifiedPolicy(in.Project)
		if err != nil {
			out[i].Err = err
			continue
		}
		if err := checkPolicyPackage(policy, in.Set, pkg); err != nil {
			out[i].Err = err
			continue
		}
		opts := InstallOptions{Force: force, WithConfig: in.WithConfig, Parts: in.Parts, Exclude: in.Exclude}
		PrepareKnownConfigOrigins(st, in.Project)
		plan, err := BuildPlan(st, repo, pkg, latest, in.Harness, in.Scope, in.Project, opts)
		if err != nil {
			out[i].Err = err
			continue
		}
		if err := checkPolicyExecutables(policy, plan.Install.Executables); err != nil {
			out[i].Err = err
			continue
		}
		if !planHasChanges(plan) {
			out[i].Err = fmt.Errorf("version %s has no installable components for %s; kept the previous install", latest.Version, in.Harness)
			continue
		}
		todo = append(todo, pending{idx: i, in: in, repo: repo, latest: latest, constraint: out[i].Constraint})
	}
	failed := false
	for i := range out {
		if out[i].Err != nil {
			failed = true
		}
	}
	if failed {
		// Nothing is applied when any package fails preflight: a corporate
		// upgrade must not half-succeed because one package is denied.
		for i := range out {
			if out[i].Err == nil && out[i].Status == "" {
				out[i].Status = "not applied"
			}
		}
		return out, nil
	}
	for _, p := range todo {
		if out[p.idx].Err != nil {
			continue
		}
		pkg, err := p.repo.LoadPackage(p.in.Package, p.latest.Version)
		if err != nil {
			out[p.idx].Err = err
			continue
		}
		opts := InstallOptions{Force: force, WithConfig: p.in.WithConfig, Parts: p.in.Parts, Exclude: p.in.Exclude}
		plan, err := BuildPlan(st, p.repo, pkg, p.latest, p.in.Harness, p.in.Scope, p.in.Project, opts)
		if err != nil {
			out[p.idx].Err = err
			continue
		}
		if err := ApplyPlan(st, plan, opts); err != nil {
			out[p.idx].Err = err
			continue
		}
		out[p.idx].Status = "upgraded"
	}
	return out, nil
}

func upgradeRepo(cfg *Config, in *Install, cache map[string]*SetRepo) (*SetRepo, error) {
	key := in.Set + "\x00" + in.SetRemote
	if r, ok := cache[key]; ok {
		return r, nil
	}
	var r *SetRepo
	var err error
	if sc, ok := cfg.Namespaces[in.Set]; ok && sc.Remote == in.SetRemote {
		r, err = OpenSet(in.Set, sc, true)
	} else if in.Set != "" && in.SetRemote != "" {
		// Keep the install's namespace name. Synthesising remote-<hash>
		// changes the install slot identity and leaves a stale managed block
		// behind, which is exactly the fresh-lock-only case.
		r, err = openSet(in.Set, NamespaceConfig{Remote: in.SetRemote}, true, false)
	} else {
		r, err = OpenRemote(in.SetRemote, true)
	}
	if err == nil {
		cache[key] = r
	}
	return r, err
}

func filterInstalls(in []*Install, pkgs []string) []Install {
	want := map[string]bool{}
	for _, p := range pkgs {
		want[p] = true
	}
	var out []Install
	for _, i := range in {
		if len(pkgs) == 0 || want[i.Package] {
			out = append(out, *i)
		}
	}
	return out
}

func Uninstall(st *State, in *Install, force bool) ([]string, error) {
	// A resource still used by another install must survive this uninstall.
	sharedFiles := map[string]bool{}
	for _, f := range in.Files {
		for _, owner := range st.ownersOf(f.Path) {
			if owner.ID != in.ID {
				sharedFiles[f.Path] = true
				break
			}
		}
	}
	sharedBlock := st.sharedBlock(in.Block, in.ID)
	// Preflight: refuse to silently discard user changes.
	for _, f := range in.Files {
		if sharedFiles[f.Path] {
			continue
		}
		fi, err := os.Lstat(f.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if fi.IsDir() {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		if ShortHash(data, 64) != f.SHA && !force {
			return nil, fmt.Errorf("%s was modified; use --force to remove it", f.Path)
		}
	}
	if in.Block != nil && !sharedBlock && !force {
		if data, err := os.ReadFile(in.Block.File); err == nil {
			if _, bs, be, _, found := findBlock(string(data), in.Block.ID); found {
				if blockBodyHash([]byte(data[bs:be])) != in.Block.SHA {
					return nil, fmt.Errorf("instructions block %q in %s was modified; use --force", in.Block.ID, in.Block.File)
				}
			}
		}
	}
	t := newTx()
	var warnings []string
	rollback := func(err error) ([]string, error) {
		t.rollback()
		return nil, err
	}
	for _, e := range in.Edits {
		if st.otherConfigEdit(in, e.File, e.Path) != nil {
			warnings = append(warnings, fmt.Sprintf("left shared config entry in %s (%s)", e.File, strings.Join(e.Path, ".")))
		}
		ok, warn, err := removeConfigEdit(t, st, in, e)
		if err != nil {
			return rollback(err)
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("left modified config entry in %s (%s)", e.File, strings.Join(e.Path, ".")))
		}
		_ = warn
	}
	if in.Block != nil && !sharedBlock {
		if err := removeBlockTx(t, in.Block, force); err != nil {
			return rollback(err)
		}
		if data, err := os.ReadFile(in.Block.File); err == nil && strings.TrimSpace(string(data)) == "" &&
			!st.fileHasOtherBlock(in, in.Block.File) && !st.fileHasOtherConfigEdits(in, in.Block.File) &&
			st.fileCreatedByAlso(in.Block.File) {
			if err := t.remove(in.Block.File); err != nil {
				return rollback(err)
			}
		}
	} else if sharedBlock {
		warnings = append(warnings, "left shared instructions block in "+in.Block.File)
	}
	for _, f := range in.Files {
		if sharedFiles[f.Path] {
			warnings = append(warnings, "left shared file "+f.Path)
			continue
		}
		if force {
			if err := t.backup(f.Path); err != nil {
				return rollback(err)
			}
		}
		if err := t.remove(f.Path); err != nil {
			return rollback(err)
		}
		pruneEmptyDirs(f.Path, pruneStops(in)...)
	}
	if err := t.snapshot(StatePath()); err != nil {
		return rollback(err)
	}
	st.Remove(in.ID)
	if err := SaveState(st); err != nil {
		return rollback(err)
	}
	if err := CleanupConfigOrigins(in.Project, st); err != nil {
		warnings = append(warnings, "could not clean config origins: "+err.Error())
	}
	return warnings, nil
}

type ReshapeResult struct {
	Install  Install
	To       string
	Status   string
	Warnings []string
	Err      error
}

// Reshape moves installed packages to another harness. It is project-scoped by
// default: --all is the explicit way to rewrite every project on the machine.
func Reshape(cfg *Config, st *State, to, project string, all bool, pkgs []string, force bool) ([]ReshapeResult, error) {
	target, ok := ResolveHarness(to)
	if !ok {
		return nil, fmt.Errorf("unknown harness %q; available: %v", to, HarnessIDs())
	}
	installs := filterInstalls(st.InstallsOf(""), pkgs)
	if !all {
		if project == "" {
			return nil, fmt.Errorf("reshape needs a project; use --dir or --all")
		}
		want := PathKey(project)
		var scoped []Install
		for _, in := range installs {
			if in.Scope == "global" || PathKey(in.Project) != want {
				continue
			}
			scoped = append(scoped, in)
		}
		installs = scoped
	}
	if len(installs) == 0 {
		if all {
			return nil, fmt.Errorf("no matching installs")
		}
		return nil, fmt.Errorf("no matching installs in %s; use --all to reshape every project", project)
	}
	repos := map[string]*SetRepo{}
	var out []ReshapeResult
	var steps []reshapeStep
	failed := false
	for _, in := range installs {
		res := ReshapeResult{Install: in, To: target.ID}
		if in.Harness == target.ID {
			res.Status = "already " + target.ID
			out = append(out, res)
			continue
		}
		repo, err := upgradeRepo(cfg, &in, repos)
		if err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		pkg, err := repo.LoadPackage(in.Package, in.Version)
		if err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		policy, err := LoadVerifiedPolicy(in.Project)
		if err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		if err := checkPolicyPackage(policy, in.Set, pkg); err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		version := VersionInfo{Version: in.Version, Label: in.Label}
		if in.Semver != "" {
			version.Semver = []string{in.Semver}
		}
		opts := InstallOptions{Force: force, WithConfig: in.WithConfig, Parts: in.Parts, Exclude: in.Exclude, OldHarness: in.Harness}
		PrepareKnownConfigOrigins(st, in.Project)
		plan, err := BuildPlan(st, repo, pkg, version, target.ID, in.Scope, in.Project, opts)
		if err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		if err := checkPolicyExecutables(policy, plan.Install.Executables); err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		if !planHasChanges(plan) {
			res.Err = fmt.Errorf("package has no installable components for %s; kept the previous install", target.Display)
			out = append(out, res)
			failed = true
			continue
		}
		if err := ApplyConfigOrigins(st, in.Project, plan.Edits, plan.Block); err != nil {
			res.Err = err
			out = append(out, res)
			failed = true
			continue
		}
		res.Warnings = plan.Warnings
		steps = append(steps, reshapeStep{
			in: in, plan: plan, opts: opts, repo: repo, pkg: pkg, version: version,
		})
	}
	if failed {
		// Return the per-package errors so the CLI can print the exact
		// conflicting key or missing component.
		return out, nil
	}
	if err := reshapePlansConflict(steps, force); err != nil {
		return out, err
	}
	var applied []reshapeStep
	for _, s := range steps {
		res := ReshapeResult{Install: s.in, To: target.ID, Warnings: s.plan.Warnings}
		if err := ApplyPlan(st, s.plan, s.opts); err != nil {
			res.Err = err
			out = append(out, res)
			// Restore every package already moved in this run.
			for i := len(applied) - 1; i >= 0; i-- {
				a := applied[i]
				backOpts := a.opts
				backOpts.Force = true
				backOpts.OldHarness = target.ID
				backPlan, berr := BuildPlan(st, a.repo, a.pkg, a.version, a.in.Harness, a.in.Scope, a.in.Project, backOpts)
				if berr == nil && planHasChanges(backPlan) {
					if cerr := ApplyConfigOrigins(st, a.in.Project, backPlan.Edits, backPlan.Block); cerr == nil {
						if aerr := ApplyPlan(st, backPlan, backOpts); aerr == nil {
							_ = MergeConfigOrigins(a.in.Project, backPlan.Edits, backPlan.Block)
						}
					}
				}
				for j := range out {
					if out[j].Install.Package == a.in.Package && out[j].Status == "reshaped" &&
						PathKey(out[j].Install.Project) == PathKey(a.in.Project) {
						out[j].Status = "rolled back"
						out[j].Err = fmt.Errorf("rolled back after a later reshape failure")
						break
					}
				}
			}
			// New-harness origins were merged before the failure; after
			// reverting every applied package they are unowned.
			seenProject := map[string]bool{}
			for _, a := range applied {
				key := PathKey(a.in.Project)
				if key == "" || seenProject[key] {
					continue
				}
				seenProject[key] = true
				_ = CleanupConfigOrigins(a.in.Project, st)
			}
			return out, fmt.Errorf("reshape failed; applied packages were rolled back: %w", err)
		}
		if err := MergeConfigOrigins(s.in.Project, s.plan.Edits, s.plan.Block); err != nil {
			res.Err = err
			out = append(out, res)
			return out, err
		}
		res.Status = "reshaped"
		res.Warnings = s.plan.Warnings
		out = append(out, res)
		applied = append(applied, s)
	}
	seenProject := map[string]bool{}
	for _, s := range steps {
		key := PathKey(s.in.Project)
		if key == "" || seenProject[key] {
			continue
		}
		seenProject[key] = true
		_ = CleanupConfigFileOrigins(s.in.Project, st)
	}
	return out, nil
}

type reshapeStep struct {
	in      Install
	plan    *Plan
	opts    InstallOptions
	repo    *SetRepo
	pkg     *Package
	version VersionInfo
}

func reshapePlansConflict(steps []reshapeStep, force bool) error {
	if force {
		return nil
	}
	edits := map[string]string{}
	files := map[string]string{}
	for _, s := range steps {
		for _, e := range s.plan.Edits {
			if e.Kind == "json-append" {
				continue
			}
			key := e.File + "\x00" + strings.Join(e.Path, ".")
			value := string(e.Value) + "\x00" + e.Block + "\x00" + e.Marker
			if prev, ok := edits[key]; ok && prev != value {
				return fmt.Errorf("reshape entries configure %s (%s) differently; use --force or reshape one package", e.File, strings.Join(e.Path, "."))
			}
			edits[key] = value
		}
		for _, f := range s.plan.NewFiles {
			if prev, ok := files[f.Dest]; ok && prev != string(f.Data) {
				return fmt.Errorf("reshape entries write %s differently; use --force or reshape one package", f.Dest)
			}
			files[f.Dest] = string(f.Data)
		}
	}
	return nil
}

type TreeFile struct {
	Path     string `json:"path"`
	Modified bool   `json:"modified"`
	Exists   bool   `json:"exists"`
}

type TreeInstall struct {
	Install Install    `json:"install"`
	Files   []TreeFile `json:"files"`
}

func BuildTree(st *State) []TreeInstall {
	out := []TreeInstall{}
	for _, in := range st.Installs {
		ti := TreeInstall{Install: in}
		for _, f := range in.Files {
			tf := TreeFile{Path: f.Path, Exists: true}
			data, err := os.ReadFile(f.Path)
			if err != nil {
				tf.Exists = false
				tf.Modified = true
			} else if ShortHash(data, 64) != f.SHA {
				tf.Modified = true
			}
			ti.Files = append(ti.Files, tf)
		}
		out = append(out, ti)
	}
	return out
}

func pruneEmptyDirs(path string, stops ...string) {
	dir := filepath.Dir(path)
	for {
		if dir == "" || dir == "." || dir == string(filepath.Separator) {
			return
		}
		stop := false
		for _, s := range stops {
			if s != "" && dir == s {
				stop = true
				break
			}
		}
		home, _ := os.UserHomeDir()
		if stop || dir == home {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func pruneStops(in *Install) []string {
	home, _ := os.UserHomeDir()
	return []string{in.Root, in.Project, home, HomeDir()}
}

func InstalledAt(in Install) string {
	if in.InstalledAt.IsZero() {
		return "unknown"
	}
	return in.InstalledAt.Local().Format("2006-01-02 15:04")
}
