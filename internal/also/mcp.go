package also

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type MCPServer struct {
	Transport string            `json:"transport"` // stdio | http
	Command   []string          `json:"command,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	// Raw is the native server object from the source harness. It is used
	// verbatim when installing back into the same harness, preserving fields
	// also does not understand.
	Raw map[string]any `json:"-"`
}

// MCPCodec is the extension point for a native MCP encoding. New encodings
// register themselves instead of editing the installer.
type MCPCodec interface {
	Extract(configs []string, files map[string]*SnapshotFile, roots []string) (map[string]MCPServer, []string, error)
	Render(name string, s MCPServer, native bool) (MCPRender, []string)
	Plan(st *State, old *Install, file, name string, r MCPRender, force bool) (*ConfigEdit, string, error)
}

var mcpCodecs = builtinMCPCodecs()

func RegisterMCPCodec(name string, codec MCPCodec) {
	if name == "" || codec == nil {
		panic("MCP codec needs a name and implementation")
	}
	if _, exists := mcpCodecs[name]; exists {
		panic("duplicate MCP codec " + name)
	}
	mcpCodecs[name] = codec
}

func mcpCodec(format MCPFormat) (MCPCodec, bool) {
	c, ok := mcpCodecs[string(format)]
	return c, ok
}

func asStringMap(v any) map[string]string {
	out := map[string]string{}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, x := range m {
		if s, ok := x.(string); ok {
			out[k] = s
		}
	}
	return out
}

func asStringList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}

func parseMCPServer(raw map[string]any) (MCPServer, []string, error) {
	var warnings []string
	s := MCPServer{Env: map[string]string{}, Headers: map[string]string{}, Raw: raw}
	s.Env = asStringMap(raw["env"])
	if len(s.Env) == 0 {
		s.Env = asStringMap(raw["environment"])
	}
	s.Headers = asStringMap(raw["headers"])
	if len(s.Headers) == 0 {
		s.Headers = asStringMap(raw["http_headers"])
	}
	if u, ok := raw["url"].(string); ok {
		s.URL = u
	}
	if u, ok := raw["httpUrl"].(string); ok {
		s.URL = u
	}
	if c, ok := raw["command"].(string); ok {
		s.Command = append([]string{c}, asStringList(raw["args"])...)
	} else if c, ok := raw["command"].([]any); ok {
		s.Command = asStringList(c)
	}
	if c, ok := raw["cwd"].(string); ok {
		s.Cwd = c
	}
	t, _ := raw["type"].(string)
	switch {
	case t == "sse":
		s.Transport = "http"
	case s.URL != "":
		s.Transport = "http"
	case len(s.Command) > 0:
		s.Transport = "stdio"
	default:
		return s, warnings, fmt.Errorf("MCP server has neither command nor url")
	}
	if s.Transport == "stdio" && len(s.Command) == 0 {
		return s, warnings, fmt.Errorf("MCP stdio server has no command")
	}
	if s.Transport == "http" && s.URL == "" {
		return s, warnings, fmt.Errorf("MCP http server has no url")
	}
	return s, warnings, nil
}

func mcpServersFromMap(source string, m map[string]any) (map[string]MCPServer, []string, error) {
	out := map[string]MCPServer{}
	var warnings []string
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		raw, ok := m[name].(map[string]any)
		if !ok {
			continue
		}
		s, w, err := parseMCPServer(raw)
		warnings = append(warnings, w...)
		if err != nil {
			return nil, warnings, fmt.Errorf("MCP server %q: %w", name, err)
		}
		out[name] = s
	}
	return out, warnings, nil
}

// ExtractBundleMCP reads MCP servers declared in the bundle in its native layout.
func ExtractBundleMCP(h *Harness, files map[string]*SnapshotFile, bundleRoots []string) (map[string]MCPServer, []string, error) {
	if h.MCP.Format == MCPNone {
		return map[string]MCPServer{}, nil, nil
	}
	codec, ok := mcpCodec(h.MCP.Format)
	if !ok {
		return nil, nil, fmt.Errorf("MCP format %q has no codec", h.MCP.Format)
	}
	return codec.Extract(h.MCP.Configs, files, bundleRoots)
}

var secretKeyRe = regexp.MustCompile(`(?i)(token|secret|passw|api[-_]?key|authorization|credential|private[-_]?key)`)

func isEnvRef(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "{env:") || strings.Contains(v, "${") || strings.HasPrefix(v, "{{") || strings.HasPrefix(v, "$")
}

// MCPSuspiciousSecrets lists config keys whose literal values look like credentials.
func MCPSuspiciousSecrets(servers map[string]MCPServer) []string {
	var out []string
	for name, s := range servers {
		for k, v := range s.Env {
			if secretKeyRe.MatchString(k) && v != "" && !isEnvRef(v) {
				out = append(out, fmt.Sprintf("mcp %s env %s", name, k))
			}
		}
		for k, v := range s.Headers {
			if secretKeyRe.MatchString(k) && v != "" && !isEnvRef(v) {
				out = append(out, fmt.Sprintf("mcp %s header %s", name, k))
			}
		}
	}
	sort.Strings(out)
	return out
}

// MCPRender is one target-native MCP server value plus its config path.
type MCPRender struct {
	Name  string
	Value any
	TOML  string // codex only
}

func RenderMCP(target, source *Harness, servers map[string]MCPServer) ([]MCPRender, []string) {
	var out []MCPRender
	var warnings []string
	if target.MCP.Format == MCPNone {
		if len(servers) > 0 {
			warnings = append(warnings, "MCP is not supported for this harness")
		}
		return out, warnings
	}
	codec, ok := mcpCodec(target.MCP.Format)
	if !ok {
		return nil, []string{fmt.Sprintf("MCP format %q has no codec", target.MCP.Format)}
	}
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	native := source != nil && source.ID == target.ID
	for _, name := range names {
		r, w := codec.Render(name, servers[name], native)
		out = append(out, r)
		warnings = append(warnings, w...)
	}
	return out, warnings
}
