package also

import (
	"encoding/json"
	"fmt"
	"strings"
)

func builtinMCPCodecs() map[string]MCPCodec {
	return map[string]MCPCodec{
		string(MCPClaudeJSON):   mcpServersCodec{},
		string(MCPQwenJSON):     mcpServersCodec{qwen: true},
		string(MCPOpenCodeJSON): openCodeMCPCodec{},
		string(MCPCodexTOML):    codexMCPCodec{},
	}
}

// mcpServersCodec covers harnesses that store {"mcpServers": {...}} in JSONC.
type mcpServersCodec struct{ qwen bool }

func (c mcpServersCodec) Extract(configs []string, files map[string]*SnapshotFile, roots []string) (map[string]MCPServer, []string, error) {
	for _, cfg := range configs {
		data, _, ok := findBundleFile(files, roots, cfg)
		if !ok {
			continue
		}
		doc, err := ParseJSONC(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse MCP config: %w", err)
		}
		raw, _ := doc.RawAt("mcpServers")
		if len(raw) == 0 {
			return map[string]MCPServer{}, nil, nil
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, nil, err
		}
		return mcpServersFromMap("", m)
	}
	return map[string]MCPServer{}, nil, nil
}

func (c mcpServersCodec) Render(name string, s MCPServer, native bool) (MCPRender, []string) {
	if native && s.Raw != nil {
		return MCPRender{Name: name, Value: s.Raw}, nil
	}
	var warnings []string
	if rt, _ := s.Raw["type"].(string); rt == "sse" {
		warnings = append(warnings, fmt.Sprintf("MCP server %s: transport sse was converted to streamable http", name))
	}
	if s.Transport == "http" {
		v := map[string]any{"url": s.URL}
		if c.qwen {
			v = map[string]any{"httpUrl": s.URL}
		} else {
			v["type"] = "http"
		}
		if len(s.Headers) > 0 {
			v["headers"] = s.Headers
		}
		return MCPRender{Name: name, Value: v}, warnings
	}
	v := map[string]any{"command": s.Command[0]}
	if len(s.Command) > 1 {
		v["args"] = s.Command[1:]
	}
	if len(s.Env) > 0 {
		v["env"] = s.Env
	}
	return MCPRender{Name: name, Value: v}, warnings
}

func (mcpServersCodec) Plan(st *State, old *Install, file, name string, r MCPRender, force bool) (*ConfigEdit, string, error) {
	return planJSONSet(st, old, file, []string{"mcpServers", name}, r.Value, force)
}

// openCodeMCPCodec covers {"mcp": {"servers": {...}}} and the legacy shape.
type openCodeMCPCodec struct{}

func (openCodeMCPCodec) Extract(configs []string, files map[string]*SnapshotFile, roots []string) (map[string]MCPServer, []string, error) {
	out := map[string]MCPServer{}
	var warnings []string
	for _, cfgName := range configs {
		data, _, ok := findBundleFile(files, roots, cfgName)
		if !ok {
			continue
		}
		doc, err := ParseJSONC(data)
		if err != nil {
			return nil, warnings, fmt.Errorf("parse %s: %w", cfgName, err)
		}
		raw, _ := doc.RawAt("mcp", "servers")
		if len(raw) == 0 {
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
		servers, w, err := mcpServersFromMap("", m)
		warnings = append(warnings, w...)
		if err != nil {
			return nil, warnings, err
		}
		for name, s := range servers {
			out[name] = s
		}
	}
	return out, warnings, nil
}

func (openCodeMCPCodec) Render(name string, s MCPServer, native bool) (MCPRender, []string) {
	if native && s.Raw != nil {
		return MCPRender{Name: name, Value: s.Raw}, nil
	}
	var warnings []string
	if rt, _ := s.Raw["type"].(string); rt == "sse" {
		warnings = append(warnings, fmt.Sprintf("MCP server %s: transport sse was converted to streamable http", name))
	}
	if s.Transport == "http" {
		v := map[string]any{"type": "remote", "url": s.URL}
		if len(s.Headers) > 0 {
			v["headers"] = s.Headers
		}
		return MCPRender{Name: name, Value: v}, warnings
	}
	v := map[string]any{"type": "local", "command": s.Command}
	if len(s.Env) > 0 {
		v["environment"] = s.Env
	}
	if s.Cwd != "" {
		v["cwd"] = s.Cwd
	}
	return MCPRender{Name: name, Value: v}, warnings
}

func (openCodeMCPCodec) Plan(st *State, old *Install, file, name string, r MCPRender, force bool) (*ConfigEdit, string, error) {
	return planOpenCodeMCP(st, old, file, name, r.Value, force)
}

// codexMCPCodec covers [mcp_servers.<name>] TOML tables.
type codexMCPCodec struct{}

func (codexMCPCodec) Extract(configs []string, files map[string]*SnapshotFile, roots []string) (map[string]MCPServer, []string, error) {
	out := map[string]MCPServer{}
	var warnings []string
	for _, cfgName := range configs {
		data, _, ok := findBundleFile(files, roots, cfgName)
		if !ok {
			continue
		}
		root, err := parseTOMLMap(data)
		if err != nil {
			return nil, warnings, fmt.Errorf("parse %s: %w", cfgName, err)
		}
		m, ok := root["mcp_servers"].(map[string]any)
		if !ok {
			continue
		}
		servers, w, err := mcpServersFromMap("", m)
		warnings = append(warnings, w...)
		if err != nil {
			return nil, warnings, err
		}
		for name, s := range servers {
			out[name] = s
		}
	}
	return out, warnings, nil
}

func (codexMCPCodec) Render(name string, s MCPServer, native bool) (MCPRender, []string) {
	if native && s.Raw != nil {
		if block, err := tomlServerBlock(name, s.Raw); err == nil {
			return MCPRender{Name: name, TOML: block}, nil
		} else {
			return MCPRender{Name: name}, []string{fmt.Sprintf("MCP server %s: could not re-encode native TOML: %v", name, err)}
		}
	}
	var warnings []string
	if rt, _ := s.Raw["type"].(string); rt == "sse" {
		warnings = append(warnings, fmt.Sprintf("MCP server %s: transport sse was converted to streamable http", name))
	}
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
	return MCPRender{Name: name, TOML: b.String()}, warnings
}

func (codexMCPCodec) Plan(st *State, old *Install, file, name string, r MCPRender, force bool) (*ConfigEdit, string, error) {
	return planTOMLMCP(st, old, file, name, r.TOML, force)
}

func findBundleFile(files map[string]*SnapshotFile, roots []string, name string) ([]byte, string, bool) {
	for _, root := range roots {
		p := name
		if root != "" {
			p = root + "/" + name
		}
		if f, ok := files[p]; ok {
			return f.Data, p, true
		}
	}
	return nil, "", false
}

func findBundleConfig(files map[string]*SnapshotFile, roots, names []string) ([]byte, string, bool) {
	for _, name := range names {
		if data, p, ok := findBundleFile(files, roots, name); ok {
			return data, p, true
		}
	}
	return nil, "", false
}
