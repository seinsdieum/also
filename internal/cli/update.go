package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		check   bool
		jsonOut bool
	)
	c := newCmd("update", "Update also to the latest published release")
	c.Long = `Checks the latest GitHub release and replaces the current also binary after
verifying its SHA-256 checksum. On Windows the new binary is written next to
the current one because a running executable cannot be replaced.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also update [--check] [--json]")
		}
		current := also.Version
		if check {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tag, err := also.LatestTag(ctx)
			if err != nil {
				return err
			}
			available := also.CompareVersions(tag, current) > 0
			if jsonOut {
				return printJSON(map[string]any{
					"current": current, "latest": tag, "available": available,
				})
			}
			if available {
				fmt.Printf("  %s update available: %s → %s\n", yellow("↑"), current, blue(tag))
				printNextStep("also update")
			} else {
				fmt.Printf("  %s also %s is up to date\n", green("✓"), current)
			}
			return nil
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		res, err := also.ApplyUpdate(current, exe)
		if err != nil {
			return err
		}
		if jsonOut {
			return printJSON(map[string]any{
				"current": res.Current, "latest": res.Latest,
				"updated": res.Updated, "manual_path": res.ManualPath,
				"asset": res.Asset,
			})
		}
		if res.Updated {
			also.MarkUpdateSeen(res.Latest)
			fmt.Printf("  %s updated also %s → %s\n", green("✓"), res.Current, blue(res.Latest))
			return nil
		}
		if res.ManualPath != "" {
			also.MarkUpdateSeen(res.Latest)
			fmt.Printf("  %s downloaded %s\n", green("✓"), res.ManualPath)
			printNextStep("close also and replace the running binary with this file")
			return nil
		}
		fmt.Printf("  %s also %s is up to date\n", green("✓"), current)
		return nil
	}
	c.Flags().BoolVar(&check, "check", false, "only check whether a newer release exists")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

// maybeNotifyUpdate prints one short notice per update interval. It runs after
// a successful command, on stderr, so JSON output stays clean.
func maybeNotifyUpdate() {
	if os.Getenv("ALSO_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return
	}
	for _, arg := range os.Args[1:] {
		if arg == "update" {
			return
		}
	}
	latest, notify, err := also.CheckLatestVersion(also.Version, time.Now(), false)
	if err != nil || !notify {
		return
	}
	fmt.Fprintf(os.Stderr, "\n  %s New version available: %s → run %s\n\n",
		yellow("↑"), blue(latest), bold("also update"))
}
