package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"also/internal/also"

	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var (
		setFlag  string
		from     string
		harness  string
		scope    string
		project  string
		force    bool
		offline  bool
		noConfig bool
	)
	c := newCmd("install <package>[@version]", "Materialize a package for a harness")
	c.Long = `Decodes a package into the native layout of the target harness. By default the
scope comes from config (project); use --scope global for user-wide installs.
Instructions are installed as a managed block; skills, commands and agents as
files; MCP and hooks are merged into native config with reversible bookkeeping.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("usage: also install <package>[@version]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		harnessID, err := cfg.Harness(harness)
		if err != nil {
			return err
		}
		scopeID, err := cfg.Scope(scope)
		if err != nil {
			return err
		}
		proj := project
		if scopeID == "project" && proj == "" {
			proj, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		return withLock(func() error {
			plan, _, err := also.InstallPackage(cfg, also.InstallRequest{
				Package: args[0], Harness: harnessID, Scope: scopeID, Project: proj,
				Force: force, WithConfig: !noConfig, Offline: offline,
			}, setFlag, from)
			if err != nil {
				return err
			}
			printPlan(plan)
			return nil
		})
	}
	c.Flags().StringVarP(&setFlag, "set", "s", "", "set name")
	c.Flags().StringVar(&from, "from", "", "install from a remote git URL without configuring a set")
	c.Flags().StringVarP(&harness, "harness", "h", "", "target harness (claude, opencode, codex, qwen)")
	c.Flags().StringVar(&scope, "scope", "", "global or project (default from config)")
	c.Flags().StringVarP(&project, "project", "p", "", "project directory for --scope project (default: cwd)")
	c.Flags().BoolVar(&force, "force", false, "overwrite conflicting files and modified content (backups are kept)")
	c.Flags().BoolVar(&offline, "offline", false, "use the local clone without fetching")
	c.Flags().BoolVar(&noConfig, "no-config", false, "do not install MCP servers or hooks")
	return c
}

func printPlan(plan *also.Plan) {
	fmt.Printf("installed %s@%s", plan.Install.Package, short(plan.Install.Version))
	if plan.Install.Label != "" {
		fmt.Printf(" (%s)", plan.Install.Label)
	}
	fmt.Printf(" -> %s (%s)\n", plan.Install.Harness, plan.Install.Scope)
	fmt.Printf("root:  %s\n", plan.Install.Root)
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
		check bool
		force bool
	)
	c := newCmd("upgrade [package...]", "Upgrade installed packages to their latest version")
	c.Long = "Upgrades packages for the harness each one was installed for. Content is\ncontent-addressed: 'up to date' means the installed digest is still the newest."
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		return withLock(func() error {
			results, err := also.UpgradeInstalls(cfg, st, args, force, check)
			if err != nil {
				return err
			}
			failed := 0
			w := newTabWriter()
			fmt.Fprintf(w, "PACKAGE\tHARNESS\tSCOPE\tOLD\tNEW\tSTATUS\n")
			for _, r := range results {
				status := r.Status
				if r.Err != nil {
					status = "error: " + r.Err.Error()
					failed++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Install.Package, r.Install.Harness, r.Install.Scope, short(r.Old), short(r.New), status)
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
	return c
}

func newReshapeCmd() *cobra.Command {
	var force bool
	c := newCmd("reshape <harness> [package...]", "Re-materialize installed packages for another harness")
	c.Long = `Transforms installed packages into the layout of another harness, keeping the
installed version and scope. Files are transactional: on failure the previous
harness layout is restored.`
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
			w := newTabWriter()
			fmt.Fprintf(w, "PACKAGE\tFROM\tTO\tSCOPE\tSTATUS\n")
			for _, r := range results {
				status := r.Status
				if r.Err != nil {
					status = "error: " + r.Err.Error()
					failed++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Install.Package, r.Install.Harness, r.To, r.Install.Scope, status)
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
	return c
}

func newTreeCmd() *cobra.Command {
	var (
		jsonOut bool
		scope   string
		harness string
	)
	c := newCmd("tree", "Show only the installed AI infrastructure")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("tree takes no arguments")
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		if harness != "" {
			if _, ok := also.ResolveHarness(harness); !ok {
				return fmt.Errorf("unknown harness %q; available: %v", harness, also.HarnessIDs())
			}
		}
		report := also.BuildTree(st)
		if scope != "" || harness != "" {
			var filtered []also.TreeInstall
			for _, t := range report {
				if scope != "" && t.Install.Scope != scope {
					continue
				}
				if harness != "" {
					h, ok := also.ResolveHarness(harness)
					if !ok || t.Install.Harness != h.ID {
						continue
					}
				}
				filtered = append(filtered, t)
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
			if a.Scope != b.Scope {
				return a.Scope < b.Scope
			}
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
			group := in.Scope + " " + in.Harness + " " + in.Root
			if group != lastGroup {
				fmt.Printf("\n%s  [%s, %s]\n", orDash(in.Root), in.Scope, in.Harness)
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
	c.Flags().StringVar(&scope, "scope", "", "filter by scope")
	c.Flags().StringVar(&harness, "harness", "", "filter by harness")
	return c
}

func newUninstallCmd() *cobra.Command {
	var (
		force   bool
		all     bool
		scope   string
		harness string
		project string
	)
	c := newCmd("uninstall <package>", "Remove an installed package and its reversible config edits")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("usage: also uninstall <package>")
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		var matches []*also.Install
		for _, in := range st.InstallsOf(args[0]) {
			if scope != "" && in.Scope != scope {
				continue
			}
			if harness != "" {
				h, ok := also.ResolveHarness(harness)
				if !ok || in.Harness != h.ID {
					continue
				}
			}
			if project != "" {
				p, err := also.AbsPath(project)
				if err != nil {
					return err
				}
				if in.Project != p {
					continue
				}
			}
			matches = append(matches, in)
		}
		if len(matches) == 0 {
			return fmt.Errorf("package %q is not installed with those filters", args[0])
		}
		if len(matches) > 1 && !all {
			var lines []string
			for _, in := range matches {
				lines = append(lines, fmt.Sprintf("  %s %s (%s)", in.Harness, in.Scope, in.Root))
			}
			return fmt.Errorf("package %q is installed %d times; add --all or filter with --scope/--harness:\n%s", args[0], len(matches), strings.Join(lines, "\n"))
		}
		ids := make([]string, 0, len(matches))
		for _, in := range matches {
			ids = append(ids, in.ID)
		}
		return withLock(func() error {
			for _, id := range ids {
				in := st.FindByID(id)
				if in == nil {
					continue
				}
				warnings, err := also.Uninstall(st, in, force)
				if err != nil {
					return err
				}
				fmt.Printf("uninstalled %s/%s@%s from %s (%s)\n", in.Set, in.Package, short(in.Version), in.Harness, in.Scope)
				printWarnings("warnings", warnings)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&force, "force", false, "remove locally modified files too")
	c.Flags().BoolVar(&all, "all", false, "remove every matching installation")
	c.Flags().StringVar(&scope, "scope", "", "filter by scope")
	c.Flags().StringVar(&harness, "harness", "", "filter by harness")
	c.Flags().StringVarP(&project, "project", "p", "", "filter by project directory")
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
	)
	c := newCmd("adopt <project-dir>", "Capture a live project's infrastructure into a bundle and push it")
	c.Long = `Extracts project-scoped instructions, skills, commands, agents, plugins, hook
scripts and native config for one harness, writes the bundle into your local
ai-infra source directory, and pushes a new package version.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("usage: also adopt <project-dir>")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		proj, err := also.AbsPath(args[0])
		if err != nil {
			return err
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
		dest := into
		var setName string
		var sc also.SetConfig
		if !noPush {
			setName, sc, err = requireSet(cmd, cfg, setFlag, "")
			if err != nil {
				return err
			}
		} else if setFlag == "" {
			if cfg.DefaultSet != "" {
				if s, ok := cfg.Sets[cfg.DefaultSet]; ok {
					sc = s
					setName = cfg.DefaultSet
				}
			}
		}
		if dest == "" {
			dest = sc.Source
		}
		if dest == "" {
			return fmt.Errorf("no local ai-infra directory; pass --into <dir> or configure one with: also set <name> --source <dir>")
		}
		if name == "" {
			name = filepath.Base(proj)
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
			fmt.Printf("captured %s -> %s\n", harnessID, res.BundleDir)
			fmt.Printf("files: %d\n", res.Files)
			printWarnings("warnings", res.Warnings)
			if noPush {
				return nil
			}
			repo, err := also.OpenSet(setName, sc, false)
			if err != nil {
				return err
			}
			pushRes, err := repo.Push(res.BundleDir, also.PushOptions{
				Name: name, Description: desc, Harness: harnessID, Message: message,
				AllowSecrets: allowSecret, AllowLarge: allowLarge,
			})
			if err != nil {
				return err
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
	c.Flags().StringVar(&name, "name", "", "package name (default: project directory name)")
	c.Flags().StringVarP(&harness, "harness", "h", "", "source harness; auto-detected by default")
	c.Flags().StringVar(&into, "into", "", "local ai-infra directory for the extracted bundle")
	c.Flags().StringVarP(&setFlag, "set", "s", "", "set name")
	c.Flags().BoolVar(&noPush, "no-push", false, "only write the local bundle")
	c.Flags().BoolVar(&force, "force", false, "replace an existing bundle (kept as a backup)")
	c.Flags().StringVarP(&message, "message", "m", "", "git commit message")
	c.Flags().StringVar(&desc, "description", "", "package description")
	c.Flags().BoolVar(&allowSecret, "allow-secrets", false, "allow likely secrets")
	c.Flags().BoolVar(&allowLarge, "allow-large", false, "allow files larger than 8 MiB")
	return c
}
