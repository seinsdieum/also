package cli

import (
	"fmt"
	"strings"

	"github.com/seinsdieum/also/internal/also"
)

// This file holds the natural-language grammar used by the CLI. Flags remain
// available for scripts, but every primary command accepts a short English
// phrase: "install skill review from personal@infra for opencode".

type pkgRef struct {
	Namespace string
	Package   string
	Version   string
}

func (r pkgRef) spec() string {
	if r.Version == "" {
		return r.Package
	}
	return r.Package + "@" + r.Version
}

func (r pkgRef) String() string {
	if r.Namespace == "" {
		return r.spec()
	}
	return r.Namespace + "@" + r.spec()
}

// parsePkgRef accepts package, package@version, namespace@package and
// namespace@package@version. A two-part token is a namespace only when the
// left side names a configured namespace.
func parsePkgRef(tok string, cfg *also.Config) (pkgRef, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return pkgRef{}, fmt.Errorf("package name is empty")
	}
	parts := strings.Split(tok, "@")
	var ref pkgRef
	switch len(parts) {
	case 1:
		ref.Package = parts[0]
	case 2:
		if cfg != nil {
			if _, ok := cfg.Namespaces[parts[0]]; ok && !looksLikeVersion(parts[1]) {
				ref.Namespace, ref.Package = parts[0], parts[1]
				break
			}
		}
		ref.Package, ref.Version = parts[0], parts[1]
	case 3:
		ref.Namespace, ref.Package, ref.Version = parts[0], parts[1], parts[2]
	default:
		return pkgRef{}, fmt.Errorf("invalid package reference %q; use [namespace@]package[@version]", tok)
	}
	if _, _, err := also.ParsePkgSpec(ref.spec()); err != nil {
		return pkgRef{}, err
	}
	if ref.Namespace != "" && !also.ValidName(ref.Namespace) {
		return pkgRef{}, fmt.Errorf("invalid namespace %q", ref.Namespace)
	}
	return ref, nil
}

func looksLikeVersion(s string) bool {
	if len(s) == 16 {
		for _, r := range s {
			if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
				return false
			}
		}
		return true
	}
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

type installSpec struct {
	Ref              pkgRef
	Parts            []string
	Harness          string
	HarnessRequested bool
}

func tokenIndex(tokens []string, want string) int {
	for i, t := range tokens {
		if strings.EqualFold(t, want) {
			return i
		}
	}
	return -1
}

// parseTrailingFor extracts "for <harness>" from an otherwise flag-free
// command. It is shared by push, adopt, tree and uninstall.
func parseTrailingFor(args []string) ([]string, string, error) {
	i := tokenIndex(args, "for")
	if i < 0 {
		return args, "", nil
	}
	if i+1 >= len(args) {
		return nil, "", clarifyHarness()
	}
	if i+2 < len(args) {
		return nil, "", fmt.Errorf("unexpected %q after harness %q", args[i+2], args[i+1])
	}
	return args[:i], args[i+1], nil
}

type clarification struct{ text string }

func (c *clarification) Error() string { return c.text }

func clarifyHarness() error { return clarifyHarnessFor(nil) }

func clarifyHarnessFor(cfg *also.Config) error {
	question := "Which harness? Please clarify!"
	if cfg != nil && cfg.DefaultHarness != "" {
		question = cfg.DefaultHarness + "? Please clarify!"
	}
	return &clarification{text: fmt.Sprintf("%s\nYou've got: %s\nTry: also install <package> for <harness>", question, strings.Join(also.HarnessIDs(), ", "))}
}

func unknownHarness(name string) error {
	return fmt.Errorf("I don't know harness %q. You've got: %s\nTry: also install <package> for <harness>", name, strings.Join(also.HarnessIDs(), ", "))
}

func parseInstallArgs(args []string, cfg *also.Config) (installSpec, error) {
	var out installSpec
	if i := tokenIndex(args, "for"); i >= 0 {
		out.HarnessRequested = true
		if i+1 >= len(args) {
			return out, clarifyHarnessFor(cfg)
		}
		out.Harness = args[i+1]
		if i+2 < len(args) {
			return out, fmt.Errorf("unexpected %q after harness %q", args[i+2], out.Harness)
		}
		args = args[:i]
	}
	if i := tokenIndex(args, "from"); i >= 0 {
		if i+1 != len(args)-1 {
			return out, fmt.Errorf("expected one package after 'from': also install [part kind] from [namespace@]package [for harness]")
		}
		// "install code-review from personal" is the namespace form unless the
		// left side is a component kind.
		if i == 1 && cfg != nil {
			if _, isSet := cfg.Namespaces[args[i+1]]; isSet {
				if _, isKind := also.CanonicalComponentKind(args[0]); !isKind {
					ref, err := parsePkgRef(args[0], cfg)
					if err != nil {
						return out, err
					}
					ref.Namespace = args[i+1]
					out.Ref = ref
					return out, nil
				}
			}
		}
		ref, err := parsePkgRef(args[i+1], cfg)
		if err != nil {
			return out, err
		}
		parts, err := parsePartSelector(args[:i])
		if err != nil {
			return out, err
		}
		out.Ref, out.Parts = ref, parts
		return out, nil
	}
	if len(args) != 1 {
		return out, fmt.Errorf("usage: also install [part kind] from [namespace@]package [for harness]")
	}
	ref, err := parsePkgRef(args[0], cfg)
	if err != nil {
		return out, err
	}
	out.Ref = ref
	return out, nil
}

func parsePartSelector(tokens []string) ([]string, error) {
	switch len(tokens) {
	case 0:
		return nil, nil
	case 1:
		kind, ok := also.CanonicalComponentKind(tokens[0])
		if !ok {
			return nil, fmt.Errorf("which kind of part is %q? try: skill, command, agent, plugin, hook, mcp, settings, instructions", tokens[0])
		}
		return []string{kind}, nil
	case 2:
		if kind, ok := also.CanonicalComponentKind(tokens[1]); ok {
			return []string{kind + ":" + tokens[0]}, nil
		}
		if kind, ok := also.CanonicalComponentKind(tokens[0]); ok {
			return []string{kind + ":" + tokens[1]}, nil
		}
		return nil, fmt.Errorf("unknown part kind %q; try: skill, command, agent, plugin, hook, mcp, settings", tokens[1])
	default:
		return nil, fmt.Errorf("a part is '<name> <kind>', for example: also install reviewer skill from personal@infra")
	}
}

type ignoreSpec struct {
	Kind      also.IgnoreKind
	Harnesses []string
	Dir       string
}

func parseIgnoreArgs(args []string) (ignoreSpec, error) {
	var out ignoreSpec
	if len(args) == 0 {
		return out, fmt.Errorf("usage: also exclude|include infra|you [for harness] [in directory]")
	}
	switch strings.ToLower(args[0]) {
	case "infra", "infrastructure":
		out.Kind = also.IgnoreInfra
	case "you", "also":
		out.Kind = also.IgnoreAlso
	default:
		return out, fmt.Errorf("what should I exclude? use infra (harness files) or you (also metadata)")
	}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch strings.ToLower(rest[i]) {
		case "for":
			i++
			if i >= len(rest) {
				return out, fmt.Errorf("which harness? You've got: %s", strings.Join(also.HarnessIDs(), ", "))
			}
			for ; i < len(rest) && !strings.EqualFold(rest[i], "in"); i++ {
				for _, name := range strings.Split(rest[i], ",") {
					if name = strings.TrimSpace(name); name != "" {
						out.Harnesses = append(out.Harnesses, name)
					}
				}
			}
			i--
		case "in":
			i++
			if i >= len(rest) {
				return out, fmt.Errorf("which directory? for example: also exclude infra in .")
			}
			out.Dir = rest[i]
		default:
			return out, fmt.Errorf("unexpected %q; usage: also exclude|include infra|you [for harness] [in directory]", rest[i])
		}
	}
	if out.Kind == also.IgnoreAlso && len(out.Harnesses) > 0 {
		return out, fmt.Errorf("'you' is not harness-specific; drop 'for <harness>'")
	}
	return out, nil
}
