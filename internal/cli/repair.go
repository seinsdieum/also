package cli

import (
	"fmt"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	var (
		all bool
		yes bool
	)
	c := newCmd("repair [namespace]", "Re-clone internal namespace caches")
	c.Long = `Deletes the internal clone for one namespace (or --all) and clones it again.
This is safe for the cache but discards unpushed commits made inside it; packages
are still taken from the remote. Use it when doctor reports a dirty or broken
clone.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 || (all && len(args) == 1) {
			return fmt.Errorf("usage: also repair [namespace|--all]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		names := cfg.NamespaceNames()
		if len(args) == 1 {
			names = []string{args[0]}
		} else if !all {
			return fmt.Errorf("pass a namespace or --all")
		}
		if len(names) == 0 {
			return fmt.Errorf("no namespaces connected")
		}
		if promptAvailable() && !yes {
			ok, err := confirm(fmt.Sprintf("Re-clone %d namespace cache(s)?", len(names)), false)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("cancelled")
			}
		}
		return withLock(func() error {
			for _, name := range names {
				ns, ok := cfg.Namespaces[name]
				if !ok {
					return fmt.Errorf("unknown namespace %q", name)
				}
				dir, err := also.RepairNamespace(name, ns)
				if err != nil {
					return err
				}
				fmt.Printf("repaired %s -> %s\n", name, dir)
			}
			return nil
		})
	}
	c.Flags().BoolVar(&all, "all", false, "re-clone every namespace")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}
