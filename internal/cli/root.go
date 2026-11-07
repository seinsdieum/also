package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/seinsdieum/also/internal/also"

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
	maybeNotifyUpdate()
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
		Long:          "also encodes AI infrastructure (instructions, skills, commands, agents, hooks and Model Context Protocol servers) into versioned packages in a git repository, and materializes them for any supported harness.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		newVersionCmd(),
		newSetupCmd(),
		newInitCmd(),
		newUpdateCmd(),
		newUseCmd(),
		newConnectCmd(),
		newConfigCmd(),
		newAddCmd(),
		newRemoveCmd(),
		newExcludeCmd(),
		newIncludeCmd(),
		newListCmd(),
		newDoctorCmd(),
		newVerifyCmd(),
		newRepairCmd(),
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
	var jsonOut bool
	c := newCmd("version", "Print the also version")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if jsonOut {
			return printJSON(map[string]string{
				"version": also.Version, "commit": also.Commit, "built": also.BuildDate,
			})
		}
		fmt.Printf("also %s\n", also.Version)
		fmt.Printf("commit: %s\n", also.Commit)
		fmt.Printf("built:  %s\n", also.BuildDate)
		return nil
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func requireSet(cmd *cobra.Command, cfg *also.Config, setFlag, remote string) (string, also.NamespaceConfig, error) {
	if remote != "" {
		name := "remote-" + also.ShortHash([]byte(remote), 6)
		return name, also.NamespaceConfig{Remote: remote}, nil
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

func addNamespaceFlag(c *cobra.Command, p *string) {
	c.Flags().StringVarP(p, "namespace", "s", "", "namespace name")
}

func newConnectCmd() *cobra.Command {
	var (
		def     bool
		force   bool
		migrate bool
		yes     bool
	)
	c := newCmd("connect <name> <git-url>", "Connect a remote namespace")
	c.Long = `Connect a git repository that stores also packages. The namespace is just a
name plus a remote URL; the local clone is an internal cache. The repository
must already exist and be reachable.

Without arguments, list connected namespaces.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			return listNamespaces(cfg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: also connect <name> <git-url>")
		}
		if !yes && promptAvailable() {
			ok, err := confirm("Connect "+args[0]+" to "+args[1]+"?", true)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("cancelled")
			}
		}
		return connectNamespace(cfg, args[0], args[1], def, force, migrate)
	}
	c.Flags().BoolVar(&def, "default", false, "make this the default namespace")
	c.Flags().BoolVar(&force, "force", false, "replace the remote URL of an existing namespace")
	c.Flags().BoolVar(&migrate, "migrate", false, "convert legacy per-version directories to one tree per package + version tags")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func listNamespaces(cfg *also.Config) error {
	if len(cfg.Namespaces) == 0 {
		fmt.Println("no namespaces configured; run: also connect <name> <git-url>")
		return nil
	}
	w := newTabWriter()
	fmt.Fprintf(w, "NAMESPACE\tREMOTE\tDEFAULT\n")
	for _, name := range cfg.NamespaceNames() {
		mark := ""
		if cfg.DefaultNamespace == name {
			mark = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, cfg.Namespaces[name].Remote, mark)
	}
	return w.Flush()
}

func connectNamespace(cfg *also.Config, name, remote string, def, force, migrate bool) error {
	if !also.ValidName(name) {
		return fmt.Errorf("invalid namespace name %q; use lowercase letters, digits, '.', '_' or '-'", name)
	}
	existing, exists := cfg.Namespaces[name]
	if exists && existing.Remote != remote && !force {
		return fmt.Errorf("namespace %q already points at %s; pass --force to replace it", name, existing.Remote)
	}
	sc := also.NamespaceConfig{Remote: remote}
	return withLock(func() error {
		repo, err := also.OpenSet(name, sc, false)
		if err != nil {
			return err
		}
		cfg.Namespaces[name] = sc
		if def || cfg.DefaultNamespace == "" {
			cfg.DefaultNamespace = name
		}
		if err := also.SaveConfig(cfg); err != nil {
			return err
		}
		if migrate {
			if err := migrateNamespace(name, sc); err != nil {
				return err
			}
		}
		fmt.Printf("connected %s -> %s\n", name, remote)
		fmt.Printf("cache: %s\n", repo.Dir)
		if cfg.DefaultNamespace == name {
			fmt.Printf("default namespace: %s\n", name)
		}
		return nil
	})
}

func migrateNamespace(name string, sc also.NamespaceConfig) error {
	repo, err := also.OpenSet(name, sc, true)
	if err != nil {
		return err
	}
	migrated, err := repo.Migrate()
	if err != nil {
		return err
	}
	if len(migrated) == 0 {
		fmt.Printf("namespace %s already uses the tree+tags layout\n", name)
		return nil
	}
	for _, m := range migrated {
		fmt.Printf("migrated %s@%s\n", m.Package, m.Version)
	}
	return nil
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
			fmt.Printf("default_namespace: %s\n", orDash(cfg.DefaultNamespace))
			fmt.Printf("default_harness:   %s\n", orDash(cfg.DefaultHarness))
			return nil
		}
		switch args[0] {
		case "get":
			if len(args) != 2 {
				return fmt.Errorf("usage: also config get <key>")
			}
			switch args[1] {
			case "default_namespace":
				fmt.Println(cfg.DefaultNamespace)
			case "default_harness":
				fmt.Println(cfg.DefaultHarness)
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
			case "default_namespace":
				if val != "" {
					if _, ok := cfg.Namespaces[val]; !ok {
						return fmt.Errorf("unknown namespace %q", val)
					}
				}
				cfg.DefaultNamespace = val
			case "default_harness":
				id, ok := also.ResolveHarness(val)
				if !ok {
					return fmt.Errorf("unknown harness %q; available: %v", val, also.HarnessIDs())
				}
				cfg.DefaultHarness = id.ID
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
