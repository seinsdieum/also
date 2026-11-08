package cli

import (
	"fmt"
	"strings"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var (
		refresh bool
		jsonOut bool
		fix     bool
	)
	c := newCmd("doctor", "Check and repair git, config, adapters, caches and installs")
	c.Long = `Runs local checks. Stale install records whose project directory no longer
exists are pruned automatically. Use --refresh to also fetch namespaces, and
--fix to re-clone a dirty or broken namespace cache. Exits non-zero when a
check fails.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also doctor [--refresh] [--fix] [--json]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		var pruned int
		var repaired []string
		if err := withLock(func() error {
			st, err := also.LoadState()
			if err != nil {
				return err
			}
			pruned = len(st.PruneMissingProjects())
			if pruned > 0 {
				if err := also.SaveState(st); err != nil {
					return err
				}
			}
			if !fix {
				return nil
			}
			for _, name := range cfg.NamespaceNames() {
				ns := cfg.Namespaces[name]
				if !also.NamespaceNeedsRepair(name, ns) {
					continue
				}
				if _, err := also.RepairNamespace(name, ns); err != nil {
					return err
				}
				repaired = append(repaired, name)
			}
			return nil
		}); err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		checks := also.Doctor(cfg, st, refresh)
		var extra []also.HealthCheck
		if pruned > 0 {
			extra = append(extra, also.HealthCheck{
				Name: "prune", Status: "ok",
				Detail: fmt.Sprintf("removed %d stale install record(s)", pruned),
			})
		}
		if len(repaired) > 0 {
			extra = append(extra, also.HealthCheck{
				Name: "repair", Status: "ok",
				Detail: "re-cloned " + strings.Join(repaired, ", "),
			})
		}
		checks = append(extra, checks...)
		if err := printChecks(checks, jsonOut); err != nil {
			return err
		}
		if also.HasFailures(checks) {
			return fmt.Errorf("doctor found failures")
		}
		return nil
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch namespaces before checking")
	c.Flags().BoolVar(&fix, "fix", false, "re-clone dirty or broken namespace caches")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func printChecks(checks []also.HealthCheck, jsonOut bool) error {
	if jsonOut {
		return printJSON(checks)
	}
	for _, c := range checks {
		mark := c.Status
		switch c.Status {
		case "ok":
			mark = "[ok]"
		case "warn":
			mark = "[warn]"
		case "fail":
			mark = "[fail]"
		}
		if c.Detail == "" {
			fmt.Printf("%-6s %s\n", mark, c.Name)
		} else if strings.Contains(c.Detail, "\n") {
			fmt.Printf("%-6s %s:\n%s\n", mark, c.Name, c.Detail)
		} else {
			fmt.Printf("%-6s %s: %s\n", mark, c.Name, c.Detail)
		}
	}
	return nil
}
