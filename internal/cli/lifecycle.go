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
		spec, err := parseInstallArgs(args, cfg)
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
			if cwd, err := os.Getwd(); err == nil {
				if _, in, err := also.FindInfra(cwd); err == nil && in.Harness != "" {
					requestedHarness = in.Harness
				}
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
		proj := project
		if proj == "" {
			proj, err = os.Getwd()
			if err != nil {
				return err
			}
			if root, _, ferr := also.FindInfra(proj); ferr == nil {
				proj = root
			}
		}
		return withLock(func() error {
			plan, _, err := also.InstallPackage(cfg, also.InstallRequest{
				Package: spec.Ref.spec(), Harness: harnessID, Scope: "project", Project: proj,
				Parts: spec.Parts, Force: force, WithConfig: !noConfig, Offline: offline,
			}, installSet, from)
			if err != nil {
				return err
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
	return c
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
		Namespace: in.Set, Parts: in.Parts, Files: len(plan.NewFiles),
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
	)
	c := newCmd("upgrade [package...]", "Upgrade packages installed in this directory")
	c.Long = "Upgrades packages installed in the current directory for the harness each one\nwas installed for. Content is content-addressed: 'up to date' means the installed\ndigest is still the newest."
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		var infra *also.Infra
		if root, in, ferr := also.FindInfra(cwd); ferr == nil {
			cwd = root
			infra = in
		}
		pkgs := args
		if len(pkgs) == 0 && infra != nil && infra.Name != "" {
			pkgs = []string{infra.Name}
		}
		return withLock(func() error {
			results, err := also.UpgradeInstallsInProject(cfg, st, cwd, pkgs, force, check, legacyGlobal)
			if err != nil {
				return err
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
	c.Flags().BoolVar(&legacyGlobal, "legacy-global", false, "report old global installs")
	_ = c.Flags().MarkHidden("legacy-global")
	return c
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
	)
	c := newCmd("reshape <harness> [package...]", "Re-materialize installed packages for another harness")
	c.Long = `Transforms installed packages into the layout of another harness, keeping the
installed version. Files are transactional: on failure the previous harness
layout is restored.`
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
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		return withLock(func() error {
			results, err := also.Reshape(cfg, st, target.ID, args[1:], force)
			if err != nil {
				return err
			}
			failed := 0
			if jsonOut {
				out := make([]reshapeOutput, 0, len(results))
				for _, r := range results {
					item := reshapeOutput{
						Package: r.Install.Package, From: r.Install.Harness,
						To: r.To, Status: r.Status,
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
			if failed > 0 {
				return fmt.Errorf("%d reshape(s) failed", failed)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite locally modified files")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

type reshapeOutput struct {
	Package string `json:"package"`
	From    string `json:"from"`
	To      string `json:"to"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
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
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		proj := project
		if proj == "" {
			proj, err = os.Getwd()
			if err != nil {
				return err
			}
			if root, _, ferr := also.FindInfra(proj); ferr == nil {
				proj = root
			}
		}
		absProj, err := also.AbsPath(proj)
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
				if in.Scope == "global" || in.Project != absProj {
					continue
				}
			}
			if harness != "" {
				h, ok := also.ResolveHarness(harness)
				if !ok || in.Harness != h.ID {
					continue
				}
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
			return fmt.Errorf("package %q is installed %d times; add --all or filter with --harness:\n%s", args[0], len(matches), strings.Join(lines, "\n"))
		}
		ids := make([]string, 0, len(matches))
		for _, in := range matches {
			ids = append(ids, in.ID)
		}
		return withLock(func() error {
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
				warnings, err := also.Uninstall(st, in, force)
				if err != nil {
					return err
				}
				item := uninstallOutput{
					Namespace: in.Set, Package: in.Package, Version: in.Version,
					Harness: in.Harness, Warnings: unique(warnings),
				}
				out = append(out, item)
				if !jsonOut {
					fmt.Printf("uninstalled %s/%s@%s from %s\n", in.Set, in.Package, short(in.Version), in.Harness)
					printWarnings("warnings", warnings)
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
	c.Flags().StringVarP(&project, "dir", "p", "", "directory to uninstall from (default: current directory)")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&legacyGlobal, "legacy-global", false, "remove an old global install")
	_ = c.Flags().MarkHidden("legacy-global")
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
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}
