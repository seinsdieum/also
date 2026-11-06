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
	)
	c := newCmd("doctor", "Check git, config, adapters, caches and installs")
	c.Long = `Runs local checks without changing anything. Use --refresh to also fetch
namespaces. Exits non-zero when a check fails.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also doctor [--refresh] [--json]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		checks := also.Doctor(cfg, st, refresh)
		if err := printChecks(checks, jsonOut); err != nil {
			return err
		}
		if also.HasFailures(checks) {
			return fmt.Errorf("doctor found failures")
		}
		return nil
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch namespaces before checking")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func newVerifyCmd() *cobra.Command {
	var (
		refresh bool
		jsonOut bool
	)
	c := newCmd("verify [namespace]", "Verify namespace contents, tags and install state")
	c.Long = `Checks package manifests, digests and version tags in each namespace, then
confirms that installed packages and versions still exist. Exits non-zero when
a check fails.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("usage: also verify [namespace] [--refresh] [--json]")
		}
		setName := ""
		if len(args) == 1 {
			setName = args[0]
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		checks := also.Verify(cfg, st, setName, refresh)
		if err := printChecks(checks, jsonOut); err != nil {
			return err
		}
		if also.HasFailures(checks) {
			return fmt.Errorf("verify found failures")
		}
		return nil
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch namespaces before verifying")
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
