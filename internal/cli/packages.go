package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		setFlag  string
		from     string
		refresh  bool
		versions bool
		jsonOut  bool
	)
	c := newCmd("list [packages|harnesses]", "List packages in a namespace or installed harnesses")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 && (args[0] == "harnesses" || args[0] == "harness") {
			return printHarnesses()
		}
		if len(args) == 1 && args[0] == "packages" {
			args = nil
		}
		if len(args) > 0 {
			return fmt.Errorf("usage: also list [packages|harnesses]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		name, sc, err := requireSet(cmd, cfg, setFlag, from)
		if err != nil {
			return err
		}
		repo, err := also.OpenSet(name, sc, refresh)
		if err != nil {
			return err
		}
		pkgs, err := repo.Packages()
		if err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		type row struct {
			Name     string             `json:"name"`
			Latest   also.VersionInfo   `json:"latest"`
			Versions []also.VersionInfo `json:"versions"`
		}
		rows := make([]row, 0, len(pkgs))
		var readErrs []string
		for _, p := range pkgs {
			vs, err := repo.Versions(p)
			if err != nil {
				readErrs = append(readErrs, p+": "+err.Error())
				continue
			}
			if len(vs) == 0 {
				readErrs = append(readErrs, p+": no versions")
				continue
			}
			rows = append(rows, row{Name: p, Latest: vs[0], Versions: vs})
		}
		if jsonOut {
			out := map[string]any{"set": name, "remote": sc.Remote, "packages": rows}
			if len(readErrs) > 0 {
				out["errors"] = readErrs
			}
			return printJSON(out)
		}
		printWarnings("unreadable packages", readErrs)
		if len(rows) == 0 {
			fmt.Printf("namespace %s is empty (remote: %s)\n", name, sc.Remote)
			return nil
		}
		if versions {
			w := newTabWriter()
			fmt.Fprintf(w, "PACKAGE\tVERSION\tLABEL\tSOURCE HARNESS\tUPDATED\tINSTALLED\n")
			for _, r := range rows {
				for _, v := range r.Versions {
					inst := ""
					for _, in := range st.Installs {
						if in.Set == name && in.Package == r.Name && in.Version == v.Version {
							inst = in.Harness
							if in.Version == r.Latest.Version {
								inst += " *"
							}
						}
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, short(v.Version), label(v.Label), v.Harness, fmtTime(v.Time), orDash(inst))
				}
			}
			return w.Flush()
		}
		w := newTabWriter()
		fmt.Fprintf(w, "PACKAGE\tLATEST\tLABEL\tSOURCE HARNESS\tUPDATED\tINSTALLED\n")
		for _, r := range rows {
			inst := ""
			for _, in := range st.Installs {
				if in.Set == name && in.Package == r.Name {
					mark := ""
					if in.Version == r.Latest.Version {
						mark = " *"
					}
					inst = in.Harness + mark
					break
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, short(r.Latest.Version), label(r.Latest.Label), r.Latest.Harness, fmtTime(r.Latest.Time), orDash(inst))
		}
		return w.Flush()
	}
	addNamespaceFlag(c, &setFlag)
	c.Flags().StringVar(&from, "from", "", "use a remote git URL without configuring a namespace")
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch the remote before listing")
	c.Flags().BoolVar(&versions, "versions", false, "show every version")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02")
}

func newShowCmd() *cobra.Command {
	var (
		setFlag string
		from    string
		refresh bool
		jsonOut bool
	)
	c := newCmd("show <package>[@version] [from <namespace>]", "Show a package manifest and its components")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 3 && strings.EqualFold(args[1], "from") {
			ns, err := namespaceName(args[2], cfg)
			if err != nil {
				return err
			}
			if setFlag != "" && setFlag != ns {
				return fmt.Errorf("namespace %q conflicts with --namespace %q", ns, setFlag)
			}
			setFlag = ns
			args = args[:1]
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: also show <package>[@version] [from <namespace>]")
		}
		ref, err := parsePkgRef(args[0], cfg)
		if err != nil {
			return err
		}
		pkgName, spec := ref.Package, ref.Version
		name, sc, err := requireSet(cmd, cfg, setFlag, from)
		if err != nil {
			return err
		}
		repo, err := also.OpenSet(name, sc, refresh)
		if err != nil {
			return err
		}
		v, err := repo.ResolveVersion(pkgName, spec)
		if err != nil {
			return err
		}
		pkg, err := repo.LoadPackage(pkgName, v.Version)
		if err != nil {
			return err
		}
		source, ok := also.ResolveHarness(pkg.Meta.SourceHarness)
		if !ok {
			return fmt.Errorf("unknown source harness %q", pkg.Meta.SourceHarness)
		}
		parts, err := also.ClassifyPackage(pkg, source)
		if err != nil {
			return err
		}
		allVersions, _ := repo.Versions(pkgName)
		roots := source.BundleRootsFromFiles(pkg.Files)
		mcp, mcpWarn, mcpErr := also.ExtractBundleMCP(source, pkg.Files, roots)
		hooks, hookWarn, hookErr := also.ExtractBundleHooks(source, pkg.Files, roots)
		for _, w := range append(mcpWarn, hookWarn...) {
			fmt.Fprintln(os.Stderr, "warning: "+w)
		}
		if mcpErr != nil {
			fmt.Fprintln(os.Stderr, "warning: cannot read MCP config: "+mcpErr.Error())
		}
		if hookErr != nil {
			fmt.Fprintln(os.Stderr, "warning: cannot read hooks config: "+hookErr.Error())
		}
		type showOut struct {
			Set          string             `json:"set"`
			Version      also.VersionInfo   `json:"version"`
			Meta         also.PackageMeta   `json:"meta"`
			Versions     []also.VersionInfo `json:"versions"`
			Files        int                `json:"files"`
			Instructions string             `json:"instructions,omitempty"`
			Skills       []string           `json:"skills"`
			Commands     []string           `json:"commands"`
			Agents       []string           `json:"agents"`
			Plugins      int                `json:"plugins"`
			HookFiles    int                `json:"hook_files"`
			MCP          []string           `json:"mcp_servers"`
			HookEvents   []string           `json:"hook_events"`
			Configs      []string           `json:"config_files"`
			Other        []string           `json:"other_files"`
		}
		out := showOut{Set: name, Version: v, Meta: pkg.Meta, Versions: allVersions, Files: len(pkg.Files), Instructions: parts.Instr, Plugins: len(parts.Plugins), HookFiles: len(parts.Hooks), Configs: parts.Configs, Other: parts.Other}
		for _, s := range parts.Skills {
			out.Skills = append(out.Skills, s.Name)
		}
		for _, x := range parts.Commands {
			out.Commands = append(out.Commands, displayRel(x, source, source.Bundle.Commands))
		}
		for _, x := range parts.Agents {
			out.Agents = append(out.Agents, displayRel(x, source, source.Bundle.Agents))
		}
		for n := range mcp {
			out.MCP = append(out.MCP, n)
		}
		for ev := range hooks {
			out.HookEvents = append(out.HookEvents, ev)
		}
		if jsonOut {
			return printJSON(out)
		}
		fmt.Printf("package:  %s@%s\n", pkg.Meta.Name, v.Version)
		if v.Label != "" {
			fmt.Printf("label:    %s\n", v.Label)
		}
		fmt.Printf("namespace: %s (%s)\n", name, sc.Remote)
		fmt.Printf("harness:  %s\n", pkg.Meta.SourceHarness)
		fmt.Printf("digest:   %s\n", pkg.Meta.Digest)
		fmt.Printf("files:    %d (%s)\n", len(pkg.Files), humanBytes(pkg.Files))
		if parts.Instr != "" {
			fmt.Printf("instructions: %s\n", parts.Instr)
		}
		summary := func(name string, vals []string) {
			if len(vals) > 0 {
				fmt.Printf("%s: %s\n", name, strings.Join(vals, ", "))
			}
		}
		var skills, cmds, agents []string
		for _, s := range parts.Skills {
			skills = append(skills, s.Name)
		}
		for _, x := range parts.Commands {
			cmds = append(cmds, displayRel(x, source, source.Bundle.Commands))
		}
		for _, x := range parts.Agents {
			agents = append(agents, displayRel(x, source, source.Bundle.Agents))
		}
		summary("skills", skills)
		summary("commands", cmds)
		summary("agents", agents)
		if len(parts.Plugins) > 0 {
			fmt.Printf("plugin files: %d\n", len(parts.Plugins))
		}
		if len(parts.Hooks) > 0 {
			fmt.Printf("hook scripts: %d\n", len(parts.Hooks))
		}
		var mcpNames, hookNames []string
		for n := range mcp {
			mcpNames = append(mcpNames, n)
		}
		for ev := range hooks {
			hookNames = append(hookNames, ev)
		}
		summary("mcp servers", mcpNames)
		summary("hook events", hookNames)
		if len(parts.Configs) > 0 {
			fmt.Printf("config files (translated only for MCP/hooks): %s\n", strings.Join(parts.Configs, ", "))
		}
		if len(parts.Other) > 0 {
			fmt.Printf("other files (not installed): %s\n", strings.Join(parts.Other, ", "))
		}
		fmt.Printf("versions: %d\n", len(allVersions))
		return nil
	}
	addNamespaceFlag(c, &setFlag)
	c.Flags().StringVar(&from, "from", "", "use a remote git URL without configuring a namespace")
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch the remote before reading")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func displayRel(rel string, h *also.Harness, dir string) string {
	for _, r := range h.Roots {
		if r != "" && strings.HasPrefix(rel, r+"/") {
			rel = strings.TrimPrefix(rel, r+"/")
			break
		}
	}
	if dir != "" && strings.HasPrefix(rel, dir+"/") {
		rel = strings.TrimPrefix(rel, dir+"/")
	}
	return rel
}

func humanBytes(files map[string]*also.SnapshotFile) string {
	var n int64
	for _, f := range files {
		n += int64(len(f.Data))
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}

func newPushCmd() *cobra.Command {
	var (
		name        string
		desc        string
		harness     string
		label       string
		message     string
		setFlag     string
		dryRun      bool
		allowSecret bool
		allowLarge  bool
		jsonOut     bool
	)
	c := newCmd("push [directory]", "Publish the initialized infrastructure in this directory")
	c.Long = `Publishes the directory marked by also.json: its name and source harness come
from the marker, so no path or harness is needed. Run "also init" first.
A directory argument is optional; the nearest initialized parent is used.
Re-running push with unchanged content is a no-op.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		args, forHarness, err := parseTrailingFor(args)
		if err != nil {
			return err
		}
		if len(args) > 1 {
			return fmt.Errorf("usage: also push [directory]")
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
		dir, infra, err := also.FindInfra(target)
		if err != nil {
			return err
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		setName, sc, err := requireSet(cmd, cfg, setFlag, "")
		if err != nil {
			return err
		}
		if name == "" {
			name = infra.Name
		}
		if !also.ValidName(name) {
			return fmt.Errorf("invalid package name %q; pass --name with lowercase letters, digits, '.', '_' or '-'", name)
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
		}
		return withLock(func() error {
			repo, err := also.OpenSet(setName, sc, false)
			if err != nil {
				return err
			}
			res, err := repo.Push(dir, also.PushOptions{
				Name: name, Description: desc, Harness: harnessID, Label: label,
				Message: message, AllowSecrets: allowSecret, AllowLarge: allowLarge, DryRun: dryRun,
			})
			if err != nil {
				return err
			}
			if dryRun {
				if jsonOut {
					return printJSON(map[string]any{"dry_run": true, "result": res})
				}
				fmt.Printf("would push %s@%s (%s) to %s\n", res.Package, short(res.Version), res.Harness, res.Remote)
				fmt.Printf("files: %d (%s)\n", res.Files, also.FormatBytes(res.Bytes))
				printWarnings("warnings", res.Warnings)
				return nil
			}
			if res.Existing {
				if jsonOut {
					return printJSON(res)
				}
				fmt.Printf("no changes: %s@%s already exists in namespace %s\n", res.Package, short(res.Version), setName)
				printWarnings("warnings", res.Warnings)
				return nil
			}
			if jsonOut {
				return printJSON(res)
			}
			fmt.Printf("pushed %s@%s", res.Package, res.Version)
			if res.Label != "" {
				fmt.Printf(" (%s)", res.Label)
			}
			fmt.Printf(" [%s]\n", res.Harness)
			fmt.Printf("files: %d (%s)\n", res.Files, also.FormatBytes(res.Bytes))
			fmt.Printf("namespace: %s\n", setName)
			printWarnings("warnings", res.Warnings)
			return nil
		})
	}
	c.Flags().StringVar(&name, "name", "", "package name (default: from also.json)")
	c.Flags().StringVar(&desc, "description", "", "package description")
	c.Flags().StringVarP(&harness, "harness", "h", "", "source harness (default: from also.json)")
	c.Flags().StringVar(&label, "label", "", "human version label, e.g. v1.2")
	c.Flags().StringVarP(&message, "message", "m", "", "git commit message")
	addNamespaceFlag(c, &setFlag)
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be pushed")
	c.Flags().BoolVar(&allowSecret, "allow-secrets", false, "allow likely secret files and literal MCP credentials")
	c.Flags().BoolVar(&allowLarge, "allow-large", false, "allow files larger than 8 MiB")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}
