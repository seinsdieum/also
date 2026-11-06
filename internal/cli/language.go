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

func newUseCmd() *cobra.Command {
	c := newCmd("use [namespace]", "Choose the active namespace")
	c.Long = `Choose which remote namespace receives pushes and adopt output, and which
namespace installs read from. Without an argument, list namespaces and mark the
active one.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			if len(cfg.Namespaces) == 0 {
				fmt.Println("no namespaces yet; add one with: also connect <name> <git-url>")
				return nil
			}
			w := newTabWriter()
			fmt.Fprintf(w, "NAMESPACE\tREMOTE\n")
			for _, name := range cfg.NamespaceNames() {
				mark := " "
				if cfg.DefaultNamespace == name {
					mark = "*"
				}
				fmt.Fprintf(w, "%s %s\t%s\n", mark, name, cfg.Namespaces[name].Remote)
			}
			return w.Flush()
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: also use <namespace>")
		}
		name := args[0]
		sc, ok := cfg.Namespaces[name]
		if !ok {
			return fmt.Errorf("I don't know namespace %q. You've got: %s", name, strings.Join(cfg.NamespaceNames(), ", "))
		}
		cfg.DefaultNamespace = name
		if err := withLock(func() error { return also.SaveConfig(cfg) }); err != nil {
			return err
		}
		fmt.Printf("using namespace %s (%s)\n", name, sc.Remote)
		return nil
	}
	return c
}

func newAddCmd() *cobra.Command {
	var (
		wantSHA string
		yes     bool
	)
	c := newCmd("add harness <file|url>", "Add a community harness adapter")
	c.Long = `Install a declarative harness adapter from a JSON file or URL. The adapter may
reuse any registered format; it cannot execute code. It is saved under
~/.also/adapters/<id>.json and validated before use.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 || !strings.EqualFold(args[0], "harness") {
			return fmt.Errorf("usage: also add harness <file|url>")
		}
		if strings.HasPrefix(args[1], "http://") || strings.HasPrefix(args[1], "https://") {
			if wantSHA == "" {
				if promptAvailable() && !yes {
					ok, err := confirm("Adapter is not pinned. Download "+args[1]+" anyway?", false)
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("cancelled; pass --sha256 <hash> to pin the adapter")
					}
				} else {
					fmt.Fprintln(os.Stderr, "warning: adapter is not pinned; pass --sha256 <hash> to verify a downloaded spec")
				}
			}
		}
		h, dest, err := also.AddAdapterFileSHA256(args[1], wantSHA)
		if err != nil {
			return err
		}
		fmt.Printf("added harness %s (%s)\n", h.ID, h.Display)
		fmt.Printf("file:  %s\n", dest)
		if len(h.Aliases) > 0 {
			fmt.Printf("also:  %s\n", strings.Join(h.Aliases, ", "))
		}
		if h.Project.Instr != "" {
			fmt.Printf("project instructions: %s\n", h.Project.Instr)
		}
		return nil
	}
	c.Flags().StringVar(&wantSHA, "sha256", "", "expected SHA-256 of the adapter file")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func newRemoveCmd() *cobra.Command {
	c := newCmd("remove harness <id>", "Remove a community harness adapter")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 || !strings.EqualFold(args[0], "harness") {
			return fmt.Errorf("usage: also remove harness <id>")
		}
		dest, err := also.RemoveAdapter(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("removed harness %s (%s)\n", args[1], dest)
		return nil
	}
	return c
}

func newExcludeCmd() *cobra.Command {
	c := newCmd("exclude <infra|you>", "Keep harness files or also metadata out of git")
	c.Long = `Adds a managed block to the project's .gitignore. 'infra' covers harness
configuration and instruction paths; 'you' covers also's own project metadata
(.also-layers, .also). Your other .gitignore lines are untouched.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runIgnore(args, true)
	}
	return c
}

func newIncludeCmd() *cobra.Command {
	c := newCmd("include <infra|you>", "Stop ignoring harness files or also metadata")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runIgnore(args, false)
	}
	return c
}

func runIgnore(args []string, add bool) error {
	spec, err := parseIgnoreArgs(args)
	if err != nil {
		return err
	}
	for _, id := range spec.Harnesses {
		if _, ok := also.ResolveHarness(id); !ok {
			return unknownHarness(id)
		}
	}
	dir := spec.Dir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	var entries []string
	if add {
		if spec.Kind == also.IgnoreInfra {
			harnesses := spec.Harnesses
			if len(harnesses) == 0 {
				harnesses, err = harnessesForProject(dir)
				if err != nil {
					return err
				}
			}
			entries = also.InfraIgnoreEntries(harnesses)
		} else {
			entries = also.AlsoIgnoreEntries()
		}
	}
	return withLock(func() error {
		changed, err := also.UpdateGitIgnore(dir, spec.Kind, add, entries)
		if err != nil {
			return err
		}
		root, _ := also.GitProjectRoot(dir)
		verb := "included"
		if add {
			verb = "excluded"
		}
		if !changed {
			fmt.Printf("%s %s already up to date in %s/.gitignore\n", verb, spec.Kind, root)
			return nil
		}
		if add {
			fmt.Printf("%s %s in %s/.gitignore\n", verb, spec.Kind, root)
			for _, e := range entries {
				fmt.Printf("  %s\n", e)
			}
		} else {
			fmt.Printf("%s %s in %s/.gitignore\n", verb, spec.Kind, root)
		}
		return nil
	})
}

func printHarnesses() error {
	list := also.Harnesses()
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	w := newTabWriter()
	fmt.Fprintf(w, "HARNESS\tNAME\tALIASES\tORIGIN\n")
	for _, h := range list {
		origin := "built-in"
		if also.IsExternalAdapter(h.ID) {
			origin = "community"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.ID, h.Display, orDash(strings.Join(h.Aliases, ", ")), origin)
	}
	return w.Flush()
}

// harnessesForProject prefers what is installed in this project, then the
// configured default, then layout detection. This keeps "exclude infra" from
// ignoring unrelated harness files.
func harnessesForProject(dir string) ([]string, error) {
	root, err := also.GitProjectRoot(dir)
	if err != nil {
		return nil, err
	}
	abs, _ := also.AbsPath(dir)
	seen := map[string]bool{}
	if st, err := also.LoadState(); err == nil {
		for _, in := range st.Installs {
			if in.Scope != "project" || in.Project == "" {
				continue
			}
			p := filepath.Clean(in.Project)
			if p == root || p == abs {
				seen[in.Harness] = true
			}
		}
	}
	if len(seen) == 0 {
		if cfg, err := also.LoadConfig(); err == nil && cfg.DefaultHarness != "" {
			if h, ok := also.ResolveHarness(cfg.DefaultHarness); ok {
				seen[h.ID] = true
			}
		}
	}
	if len(seen) == 0 {
		if id, err := also.DetectHarness(root); err == nil {
			seen[id] = true
		}
	}
	if len(seen) == 0 {
		return nil, clarifyHarness()
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
