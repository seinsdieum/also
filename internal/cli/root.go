package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"also/internal/also"

	"github.com/spf13/cobra"
)

func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		var ask *clarification
		if errors.As(err, &ask) {
			fmt.Fprintln(os.Stderr, ask.Error())
			return 1
		}
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		return 1
	}
	return 0
}

func newCmd(use, short string) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short}
	// -h is reserved for --harness, so help has no shorthand.
	c.Flags().Bool("help", false, "help for this command")
	return c
}

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "also",
		Short:         "Portable, versioned AI infrastructure bundles",
		Long:          "also encodes harness-specific AI infrastructure (instructions, skills, commands, agents, hooks, MCP) into versioned packages in a git repository, and materializes them for any supported harness.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		newVersionCmd(),
		newUseCmd(),
		newSetCmd(),
		newConfigCmd(),
		newAddCmd(),
		newRemoveCmd(),
		newExcludeCmd(),
		newIncludeCmd(),
		newListCmd(),
		newShowCmd(),
		newPushCmd(),
		newAdoptCmd(),
		newInstallCmd(),
		newUpgradeCmd(),
		newReshapeCmd(),
		newTreeCmd(),
		newUninstallCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	c := newCmd("version", "Print the also version")
	c.Run = func(cmd *cobra.Command, args []string) {
		fmt.Printf("also %s\n", also.Version)
	}
	return c
}

func requireSet(cmd *cobra.Command, cfg *also.Config, setFlag, remote string) (string, also.SetConfig, error) {
	if remote != "" {
		name := "remote-" + also.ShortHash([]byte(remote), 6)
		return name, also.SetConfig{Remote: remote, Path: also.SetCachePath(remote)}, nil
	}
	return cfg.ResolveSet(setFlag)
}

func withLock(fn func() error) error {
	lock, err := also.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

func printWarnings(title string, items []string) {
	items = unique(items)
	if len(items) == 0 {
		return
	}
	fmt.Println(title + ":")
	for _, w := range items {
		fmt.Println("  - " + w)
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func short(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}

func label(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

func newSetCmd() *cobra.Command {
	var (
		remote  string
		source  string
		def     bool
		force   bool
		migrate bool
	)
	c := newCmd("set [name]", "Configure a remote set and its local clone")
	c.Long = `Attach a git repository that stores also packages. The remote repository
must already exist. The local clone lives under ~/.also/sets/<name>.

A set can point at a local directory of hand-written bundles with --source;
it is used by 'also adopt' to write the extracted bundle back.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			if len(cfg.Sets) == 0 {
				fmt.Println("no sets configured; run: also set <name> --remote <git-url>")
				return nil
			}
			w := newTabWriter()
			fmt.Fprintf(w, "NAME\tREMOTE\tSOURCE\tDEFAULT\n")
			for _, name := range cfg.SetNames() {
				s := cfg.Sets[name]
				mark := ""
				if cfg.DefaultSet == name {
					mark = "*"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, s.Remote, orDash(s.Source), mark)
			}
			return w.Flush()
		}
		if len(args) > 1 {
			return fmt.Errorf("expected one set name")
		}
		name := args[0]
		if !also.ValidName(name) {
			return fmt.Errorf("invalid set name %q; use lowercase letters, digits, '.', '_' or '-'", name)
		}
		if migrate {
			existing, ok := cfg.Sets[name]
			if !ok {
				return fmt.Errorf("unknown set %q", name)
			}
			return withLock(func() error {
				repo, err := also.OpenSet(name, existing, true)
				if err != nil {
					return err
				}
				migrated, err := repo.Migrate()
				if err != nil {
					return err
				}
				if len(migrated) == 0 {
					fmt.Printf("set %s already uses the tree+tags layout\n", name)
					return nil
				}
				for _, m := range migrated {
					fmt.Printf("migrated %s@%s\n", m.Package, m.Version)
				}
				return nil
			})
		}
		if source != "" {
			abs, err := also.AbsPath(source)
			if err != nil {
				return err
			}
			if st, err := os.Stat(abs); err != nil || !st.IsDir() {
				return fmt.Errorf("source directory does not exist: %s", abs)
			}
			source = abs
		}
		existing, exists := cfg.Sets[name]
		if exists && remote == "" {
			// Show the set; optionally update the source.
			changed := false
			if source != "" {
				existing.Source = source
				cfg.Sets[name] = existing
				changed = true
			}
			if def {
				cfg.DefaultSet = name
				changed = true
			}
			if changed {
				if err := also.SaveConfig(cfg); err != nil {
					return err
				}
			}
			fmt.Printf("set %s\n  remote: %s\n  clone:  %s\n  source: %s\n", name, existing.Remote, existing.Path, orDash(existing.Source))
			return nil
		}
		if remote == "" {
			return fmt.Errorf("--remote <git-url> is required for a new set")
		}
		return withLock(func() error {
			sc := also.SetConfig{Remote: remote, Path: also.SettingSetPath(name), Source: source}
			if exists {
				if existing.Remote != remote && !force {
					return fmt.Errorf("set %q already points at %s; pass --force to replace the local clone", name, existing.Remote)
				}
				if existing.Remote != remote {
					fmt.Fprintf(os.Stderr, "warning: replacing local clone %s (unpushed commits will be lost)\n", existing.Path)
					if err := os.RemoveAll(existing.Path); err != nil {
						return err
					}
				}
				sc.Source = firstNonEmpty(source, existing.Source)
				sc.Path = existing.Path
				if sc.Path == "" {
					sc.Path = also.SettingSetPath(name)
				}
			}
			repo, err := also.OpenSet(name, sc, false)
			if err != nil {
				return err
			}
			cfg.Sets[name] = also.SetConfig{Remote: remote, Path: repo.Dir, Source: sc.Source}
			if def || cfg.DefaultSet == "" {
				cfg.DefaultSet = name
			}
			if err := also.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("set %s -> %s\n", name, remote)
			fmt.Printf("clone:  %s\n", repo.Dir)
			if sc.Source != "" {
				fmt.Printf("source: %s\n", sc.Source)
			}
			return nil
		})
	}
	c.Flags().StringVar(&remote, "remote", "", "git URL of the remote repository")
	c.Flags().StringVar(&source, "source", "", "local directory containing hand-written bundles")
	c.Flags().BoolVar(&def, "default", false, "make this the default set")
	c.Flags().BoolVar(&force, "force", false, "replace the local clone when the remote changes")
	c.Flags().BoolVar(&migrate, "migrate", false, "convert legacy per-version directories to one tree per package + version tags")
	return c
}

func newConfigCmd() *cobra.Command {
	c := newCmd("config [get <key> | set <key> <value>]", "Read or change also defaults")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Printf("config: %s\n", also.ConfigPath())
			fmt.Printf("state:  %s\n", also.StatePath())
			fmt.Printf("default_set:     %s\n", orDash(cfg.DefaultSet))
			fmt.Printf("default_harness: %s\n", orDash(cfg.DefaultHarness))
			fmt.Printf("default_scope:   %s\n", cfg.DefaultScope)
			return nil
		}
		switch args[0] {
		case "get":
			if len(args) != 2 {
				return fmt.Errorf("usage: also config get <key>")
			}
			switch args[1] {
			case "default_set":
				fmt.Println(cfg.DefaultSet)
			case "default_harness":
				fmt.Println(cfg.DefaultHarness)
			case "default_scope":
				fmt.Println(cfg.DefaultScope)
			default:
				return fmt.Errorf("unknown key %q", args[1])
			}
			return nil
		case "set":
			if len(args) != 3 {
				return fmt.Errorf("usage: also config set <key> <value>")
			}
			key, val := args[1], args[2]
			switch key {
			case "default_set":
				if val != "" {
					if _, ok := cfg.Sets[val]; !ok {
						return fmt.Errorf("unknown set %q", val)
					}
				}
				cfg.DefaultSet = val
			case "default_harness":
				id, ok := also.ResolveHarness(val)
				if !ok {
					return fmt.Errorf("unknown harness %q; available: %v", val, also.HarnessIDs())
				}
				cfg.DefaultHarness = id.ID
			case "default_scope":
				if val != "global" && val != "project" {
					return fmt.Errorf("default_scope must be global or project")
				}
				cfg.DefaultScope = val
			default:
				return fmt.Errorf("unknown key %q", key)
			}
			return withLock(func() error { return also.SaveConfig(cfg) })
		default:
			return fmt.Errorf("usage: also config [get <key> | set <key> <value>]")
		}
	}
	return c
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
