package cli

import (
	"fmt"
	"os"
	"strings"
)

// One accent colour across the installer and diagnostics: cobalt blue.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiBlue   = "\x1b[38;2;59;130;246m"
	ansiGreen  = "\x1b[38;2;34;197;94m"
	ansiYellow = "\x1b[38;2;234;179;8m"
	ansiRed    = "\x1b[38;2;239;68;68m"
)

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("ALSO_NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func tint(code, s string) string {
	if !colorEnabled() {
		return s
	}
	return code + s + ansiReset
}

func blue(s string) string   { return tint(ansiBlue, s) }
func bold(s string) string   { return tint(ansiBold, s) }
func dim(s string) string    { return tint(ansiDim, s) }
func green(s string) string  { return tint(ansiGreen, s) }
func yellow(s string) string { return tint(ansiYellow, s) }
func red(s string) string    { return tint(ansiRed, s) }

func printBanner() {
	if !colorEnabled() {
		fmt.Println("also — portable AI infrastructure")
		fmt.Println()
		return
	}
	fmt.Println()
	fmt.Println("  " + blue("◆") + " " + bold("also") + dim("   portable AI infrastructure"))
	fmt.Println("  " + dim(strings.Repeat("─", 44)))
	fmt.Println()
}

func printSummary(rows [][2]string) {
	for _, row := range rows {
		fmt.Printf("  %s %-11s %s\n", green("✓"), dim(row[0]), row[1])
	}
}

func printNextStep(text string) {
	fmt.Println()
	fmt.Printf("  %s %s\n\n", blue("→"), text)
}
