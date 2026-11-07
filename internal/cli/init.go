package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		harness string
		jsonOut bool
	)
	c := newCmd("init <name> [for <harness>]", "Initialize this directory as portable infrastructure")
	c.Long = `Writes also.json into the current directory. The marker records the package
name and source harness, so later commands need no path: "also push" publishes
this directory and "also adopt" captures it.

The harness is taken from "for <harness>", then from the files already present,
then from the configured default harness.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		args, forHarness, err := parseTrailingFor(args)
		if err != nil {
			return err
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: also init <name> [for <harness>]")
		}
		if forHarness != "" {
			if harness != "" && !strings.EqualFold(harness, forHarness) {
				return fmt.Errorf("two different harnesses: %q and %q", harness, forHarness)
			}
			harness = forHarness
		}
		name := args[0]
		if !also.ValidName(name) {
			return fmt.Errorf("invalid package name %q; use lowercase letters, digits, '.', '_' or '-'", name)
		}
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, _, err := also.FindInfra(dir); err == nil {
			return fmt.Errorf("this directory is already initialized; edit %s to change the name or harness", also.InfraFile)
		}
		harnessID, err := initHarness(harness, dir, name)
		if err != nil {
			return err
		}
		in := &also.Infra{Name: name, Harness: harnessID}
		if err := also.SaveInfra(dir, in); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(map[string]string{"name": name, "harness": harnessID, "file": dir + string(os.PathSeparator) + also.InfraFile})
		}
		fmt.Printf("initialized %s for %s\n", name, harnessID)
		fmt.Printf("marker: %s/%s\n", dir, also.InfraFile)
		fmt.Println("next:   also push")
		return nil
	}
	c.Flags().StringVarP(&harness, "harness", "h", "", "source harness; detected or taken from config by default")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func initHarness(explicit, dir, name string) (string, error) {
	if explicit != "" {
		h, ok := also.ResolveHarness(explicit)
		if !ok {
			return "", unknownHarness(explicit)
		}
		return h.ID, nil
	}
	if id, err := also.DetectHarness(dir); err == nil {
		return id, nil
	}
	cfg, err := also.LoadConfig()
	if err != nil {
		return "", err
	}
	if cfg.DefaultHarness != "" {
		if h, ok := also.ResolveHarness(cfg.DefaultHarness); ok {
			return h.ID, nil
		}
	}
	return "", fmt.Errorf("cannot detect a harness in this directory; run: also init %s for <harness>", name)
}
