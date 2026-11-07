package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

type prompter struct {
	r      *bufio.Reader
	closer io.Closer
}

func (p *prompter) close() {
	if p != nil && p.closer != nil {
		_ = p.closer.Close()
	}
}

func (p *prompter) ask(question, def string) (string, error) {
	fmt.Printf("  %s %s", blue("?"), question)
	if def != "" {
		fmt.Printf(" %s", dim("["+def+"]"))
	}
	fmt.Print(": ")
	line, err := p.r.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("input closed")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func (p *prompter) yes(question string, def bool) (bool, error) {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	got, err := p.ask(question+" ("+d+")", "")
	if err != nil {
		return false, err
	}
	got = strings.ToLower(got)
	if got == "" {
		return def, nil
	}
	return got == "y" || got == "yes", nil
}

func newSetupCmd() *cobra.Command {
	var (
		yes        bool
		harness    string
		namespaces []string
		defaultNS  string
		starter    string
		project    string
		jsonOut    bool
	)
	c := newCmd("setup", "Connect harnesses and namespaces for first use")
	c.Long = `Interactive first-run setup: detect installed harness CLIs, choose a default
harness, connect one or more remote namespaces, and optionally install a
starter package.

For CI or scripts use --yes plus flags:
  also setup --yes --harness opencode \
    --namespace personal=git@github.com:me/ai-infra.git \
    --default-namespace personal`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also setup [flags]")
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		interactive := !yes
		var p *prompter
		if interactive {
			in, closer, ok := openPromptInput()
			if !ok {
				return fmt.Errorf("no terminal available; pass --yes with --harness and --namespace")
			}
			p = &prompter{r: bufio.NewReader(in), closer: closer}
			defer p.close()
		}
		if interactive && !jsonOut {
			printBanner()
		}

		harnessID, err := setupHarness(cfg, harness, p)
		if err != nil {
			return err
		}
		pairs, err := setupNamespaces(cfg, namespaces, p)
		if err != nil {
			return err
		}
		if len(pairs) == 0 && len(cfg.Namespaces) == 0 {
			return fmt.Errorf("no namespaces connected; pass --namespace <name>=<git-url>")
		}
		if defaultNS == "" {
			defaultNS = cfg.DefaultNamespace
		}
		if interactive && len(pairs) > 0 {
			choices := cfg.NamespaceNames()
			for _, pair := range pairs {
				choices = append(choices, pair.name)
			}
			def := defaultNS
			if def == "" || !contains(choices, def) {
				def = choices[len(choices)-1]
			}
			defaultNS, err = p.ask("Default namespace", def)
			if err != nil {
				return err
			}
		}
		if defaultNS == "" {
			if len(pairs) == 1 {
				defaultNS = pairs[0].name
			} else {
				return fmt.Errorf("multiple namespaces; pass --default-namespace")
			}
		}
		if starter == "" && interactive {
			starter, err = p.ask("Starter package to install (leave empty to skip)", "")
			if err != nil {
				return err
			}
		}

		if err := withLock(func() error {
			for _, pair := range pairs {
				sc := also.NamespaceConfig{Remote: pair.url}
				if _, err := also.OpenSet(pair.name, sc, false); err != nil {
					return fmt.Errorf("namespace %s: %w", pair.name, err)
				}
				cfg.Namespaces[pair.name] = sc
			}
			if _, ok := cfg.Namespaces[defaultNS]; !ok {
				return fmt.Errorf("default namespace %q is not connected", defaultNS)
			}
			cfg.DefaultNamespace = defaultNS
			cfg.DefaultHarness = harnessID
			return also.SaveConfig(cfg)
		}); err != nil {
			return err
		}

		type setupOutput struct {
			Harness          string            `json:"harness"`
			DefaultNamespace string            `json:"default_namespace"`
			Namespaces       map[string]string `json:"namespaces"`
			Installed        string            `json:"installed,omitempty"`
		}
		out := setupOutput{Harness: harnessID, DefaultNamespace: defaultNS, Namespaces: map[string]string{}}
		for _, name := range cfg.NamespaceNames() {
			out.Namespaces[name] = cfg.Namespaces[name].Remote
		}
		if !jsonOut {
			rows := [][2]string{
				{"Harness", harnessID},
				{"Namespace", defaultNS},
			}
			for _, name := range cfg.NamespaceNames() {
				if name != defaultNS {
					rows = append(rows, [2]string{"Namespace", name})
				}
			}
			printSummary(rows)
		}
		if starter != "" {
			proj := project
			if proj == "" {
				proj, err = os.Getwd()
				if err != nil {
					return err
				}
				if root, _, ferr := also.FindInfra(proj); ferr == nil {
					proj = root
				}
			}
			if _, _, err := also.InstallPackage(cfg, also.InstallRequest{
				Package: starter, Harness: harnessID, Scope: "project", Project: proj, WithConfig: true,
			}, defaultNS, ""); err != nil {
				return err
			}
			out.Installed = starter
			if !jsonOut {
				fmt.Printf("installed starter package %s\n", starter)
			}
		}
		if jsonOut {
			return printJSON(out)
		}
		printNextStep("also list")
		return nil
	}
	c.Flags().BoolVar(&yes, "yes", false, "non-interactive; use flags for every choice")
	c.Flags().StringVar(&harness, "harness", "", "default harness (claude, opencode, codex, qwen, ...)")
	c.Flags().StringArrayVar(&namespaces, "namespace", nil, "namespace to connect as <name>=<git-url> (repeatable)")
	c.Flags().StringVar(&defaultNS, "default-namespace", "", "active namespace")
	c.Flags().StringVar(&starter, "install", "", "package to install after setup")
	c.Flags().StringVarP(&project, "dir", "p", "", "directory for the starter install (default: current directory)")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

type namespacePair struct{ name, url string }

func setupHarness(cfg *also.Config, flag string, p *prompter) (string, error) {
	detected := installedHarnesses()
	if flag != "" {
		h, ok := also.ResolveHarness(flag)
		if !ok {
			return "", fmt.Errorf("unknown harness %q; available: %v", flag, also.HarnessIDs())
		}
		return h.ID, nil
	}
	if p == nil {
		return "", fmt.Errorf("--harness is required with --yes; available: %v", also.HarnessIDs())
	}
	if len(detected) > 0 {
		fmt.Printf("  %s %s\n\n", dim("detected"), blue(strings.Join(detected, ", ")))
	}
	def := cfg.DefaultHarness
	if def == "" {
		if len(detected) > 0 {
			def = detected[0]
		} else {
			def = "opencode"
		}
	}
	got, err := p.ask("Default harness ("+strings.Join(also.HarnessIDs(), ", ")+")", def)
	if err != nil {
		return "", err
	}
	h, ok := also.ResolveHarness(got)
	if !ok {
		return "", fmt.Errorf("unknown harness %q; available: %v", got, also.HarnessIDs())
	}
	return h.ID, nil
}

func setupNamespaces(cfg *also.Config, flags []string, p *prompter) ([]namespacePair, error) {
	var pairs []namespacePair
	seen := map[string]bool{}
	for _, f := range flags {
		name, url, ok := strings.Cut(f, "=")
		if !ok || name == "" || url == "" {
			return nil, fmt.Errorf("--namespace must be <name>=<git-url>, got %q", f)
		}
		if !also.ValidName(name) {
			return nil, fmt.Errorf("invalid namespace name %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("namespace %q repeated", name)
		}
		seen[name] = true
		pairs = append(pairs, namespacePair{name: name, url: url})
	}
	if p == nil || len(pairs) > 0 {
		return pairs, nil
	}
	for {
		if len(cfg.Namespaces) > 0 || len(pairs) > 0 {
			ok, err := p.yes("Connect another namespace?", len(cfg.Namespaces) == 0)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
		}
		name, err := p.ask("Namespace name", "")
		if err != nil {
			return nil, err
		}
		if !also.ValidName(name) {
			return nil, fmt.Errorf("invalid namespace name %q", name)
		}
		if seen[name] {
			fmt.Printf("namespace %s is already connected\n", name)
			continue
		}
		url, err := p.ask("Git URL for "+name, "")
		if err != nil {
			return nil, err
		}
		if url == "" {
			return nil, fmt.Errorf("git URL is required")
		}
		seen[name] = true
		pairs = append(pairs, namespacePair{name: name, url: url})
	}
	return pairs, nil
}

func installedHarnesses() []string {
	var out []string
	for _, h := range also.Harnesses() {
		if h.GlobalBase != "" {
			if st, err := os.Stat(also.ExpandHome(h.GlobalBase)); err == nil && st.IsDir() {
				out = append(out, h.ID)
				continue
			}
		}
		for _, bin := range harnessBinaries[h.ID] {
			if _, err := exec.LookPath(bin); err == nil {
				out = append(out, h.ID)
				break
			}
		}
	}
	return out
}

var harnessBinaries = map[string][]string{
	"claude":   {"claude"},
	"opencode": {"opencode"},
	"codex":    {"codex"},
	"qwen":     {"qwen", "qwen-code"},
}

func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// openPromptInput lets `curl | sh` hand the terminal to setup: stdin is the
// script pipe there, so read prompts from /dev/tty instead.
func openPromptInput() (io.Reader, io.Closer, bool) {
	if stdinIsTerminal() {
		return os.Stdin, nil, true
	}
	f, err := os.Open("/dev/tty")
	if err != nil {
		return nil, nil, false
	}
	return f, f, true
}

func promptAvailable() bool {
	if stdinIsTerminal() {
		return true
	}
	f, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func confirm(question string, def bool) (bool, error) {
	in, closer, ok := openPromptInput()
	if !ok {
		return def, nil
	}
	p := &prompter{r: bufio.NewReader(in), closer: closer}
	defer p.close()
	return p.yes(question, def)
}
