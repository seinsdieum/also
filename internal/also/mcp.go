package also

import (
	"encoding/json"
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
	out := map[string]MCPServer{}
	var warnings []string
	merge := func(name string, servers map[string]MCPServer, warnings2 []string) error {
		warnings = append(warnings, warnings2...)
		for n, s := range servers {
			if prev, ok := out[n]; ok {
				a, _ := json.Marshal(prev)
				b, _ := json.Marshal(s)
				if string(a) != string(b) {
					return fmt.Errorf("MCP server %q is defined twice with different settings", n)
				}
			}
			out[n] = s
		}
		return nil
	}
	find := func(candidates ...string) ([]byte, string, bool) {
		for _, root := range bundleRoots {
			for _, c := range candidates {
				p := c
				if root != "" {
					p = root + "/" + c
				}
				if f, ok := files[p]; ok {
					return f.Data, p, true
				}
			}
		}
		return nil, "", false
	}

	switch h.MCP.Format {
	case MCPClaudeJSON, MCPQwenJSON:
		var data []byte
		var ok bool
		for _, p := range h.MCP.Configs {
			if d, _, found := find(p); found {
				data, ok = d, true
				break
			}
		}
		if ok {
			doc, err := ParseJSONC(data)
			if err != nil {
				return nil, warnings, fmt.Errorf("parse MCP config: %w", err)
			}
			raw, _ := doc.RawAt("mcpServers")
			if len(raw) > 0 {
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err != nil {
					return nil, warnings, err
				}
				servers, w, err := mcpServersFromMap(string(h.MCP.Format), m)
				if err != nil {
					return nil, warnings, err
				}
				if err := merge("", servers, w); err != nil {
					return nil, warnings, err
				}
			}
		}
	case MCPOpenCodeJSON:
		for _, cfgName := range h.MCP.Configs {
			data, _, ok := find(cfgName)
			if !ok {
				continue
			}
			doc, err := ParseJSONC(data)
			if err != nil {
				return nil, warnings, fmt.Errorf("parse %s: %w", cfgName, err)
			}
			raw, _ := doc.RawAt("mcp", "servers")
			if len(raw) == 0 {
				// Legacy V1 shape: mcp.<name> directly under mcp.
				raw, _ = doc.RawAt("mcp")
				if len(raw) > 0 {
					var probe map[string]any
					if json.Unmarshal(raw, &probe) == nil {
						if _, hasServers := probe["servers"]; !hasServers {
							warnings = append(warnings, "opencode MCP uses the legacy mcp.<name> shape; consider migrating to mcp.servers")
						}
					}
				}
			}
			if len(raw) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, warnings, err
			}
			if _, hasServers := m["servers"]; hasServers {
				if sm, ok := m["servers"].(map[string]any); ok {
					m = sm
				}
			}
			servers, w, err := mcpServersFromMap(string(h.MCP.Format), m)
			if err != nil {
				return nil, warnings, err
			}
			if err := merge("", servers, w); err != nil {
				return nil, warnings, err
			}
		}
	case MCPCodexTOML:
		for _, cfgName := range h.MCP.Configs {
			data, _, ok := find(cfgName)
			if !ok {
				continue
			}
			root, err := parseTOMLMap(data)
			if err != nil {
				return nil, warnings, fmt.Errorf("parse %s: %w", cfgName, err)
			}
			if m, ok := root["mcp_servers"].(map[string]any); ok {
				servers, w, err := mcpServersFromMap(string(h.MCP.Format), m)
				if err != nil {
					return nil, warnings, err
				}
				if err := merge("", servers, w); err != nil {
					return nil, warnings, err
				}
			}
		}
	}
	return out, warnings, nil
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
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		s := servers[name]
		if source != nil && source.ID == target.ID && s.Raw != nil {
			if target.MCP.Format == MCPCodexTOML {
				block, err := tomlServerBlock(name, s.Raw)
				if err == nil {
					out = append(out, MCPRender{Name: name, TOML: block})
					continue
				}
				warnings = append(warnings, fmt.Sprintf("MCP server %s: could not re-encode native TOML: %v", name, err))
			} else {
				out = append(out, MCPRender{Name: name, Value: s.Raw})
				continue
			}
		}
		if rt, _ := s.Raw["type"].(string); rt == "sse" {
			warnings = append(warnings, fmt.Sprintf("MCP server %s: transport sse was converted to streamable http", name))
		}
		switch target.MCP.Format {
		case MCPClaudeJSON:
			if s.Transport == "http" {
				v := map[string]any{"type": "http", "url": s.URL}
				if len(s.Headers) > 0 {
					v["headers"] = s.Headers
				}
				out = append(out, MCPRender{Name: name, Value: v})
			} else {
				v := map[string]any{"command": s.Command[0]}
				if len(s.Command) > 1 {
					v["args"] = s.Command[1:]
				}
				if len(s.Env) > 0 {
					v["env"] = s.Env
				}
				out = append(out, MCPRender{Name: name, Value: v})
			}
		case MCPQwenJSON:
			if s.Transport == "http" {
				v := map[string]any{"httpUrl": s.URL}
				if len(s.Headers) > 0 {
					v["headers"] = s.Headers
				}
				out = append(out, MCPRender{Name: name, Value: v})
			} else {
				v := map[string]any{"command": s.Command[0]}
				if len(s.Command) > 1 {
					v["args"] = s.Command[1:]
				}
				if len(s.Env) > 0 {
					v["env"] = s.Env
				}
				out = append(out, MCPRender{Name: name, Value: v})
			}
		case MCPOpenCodeJSON:
			if s.Transport == "http" {
				v := map[string]any{"type": "remote", "url": s.URL}
				if len(s.Headers) > 0 {
					v["headers"] = s.Headers
				}
				out = append(out, MCPRender{Name: name, Value: v})
			} else {
				v := map[string]any{"type": "local", "command": s.Command}
				if len(s.Env) > 0 {
					v["environment"] = s.Env
				}
				if s.Cwd != "" {
					v["cwd"] = s.Cwd
				}
				out = append(out, MCPRender{Name: name, Value: v})
			}
		case MCPCodexTOML:
			var b strings.Builder
			b.WriteString("[mcp_servers.")
			b.WriteString(tomlKey(name))
			b.WriteString("]\n")
			if s.Transport == "http" {
				b.WriteString("url = " + tomlString(s.URL) + "\n")
				if len(s.Headers) > 0 {
					b.WriteString("http_headers = " + tomlInlineTable(s.Headers) + "\n")
				}
			} else {
				b.WriteString("command = " + tomlString(s.Command[0]) + "\n")
				if len(s.Command) > 1 {
					b.WriteString("args = " + tomlStringArray(s.Command[1:]) + "\n")
				}
				if len(s.Env) > 0 {
					b.WriteString("env = " + tomlInlineTable(s.Env) + "\n")
				}
			}
			out = append(out, MCPRender{Name: name, TOML: b.String()})
		default:
			warnings = append(warnings, "MCP is not supported for this harness")
		}
	}
	return out, warnings
}
