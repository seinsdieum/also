package also

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var toolNames = map[string]string{
	"read": "Read", "write": "Write", "edit": "Edit", "bash": "Bash",
	"grep": "Grep", "glob": "Glob", "webfetch": "WebFetch", "websearch": "WebSearch",
	"task": "Task", "todowrite": "TodoWrite", "notebookedit": "NotebookEdit",
	"list": "List", "patch": "Patch",
}

func canonicalTool(name string) (string, bool) {
	l := strings.ToLower(strings.TrimSpace(name))
	if c, ok := toolNames[l]; ok {
		return c, true
	}
	for _, c := range toolNames {
		if strings.EqualFold(c, name) {
			return c, true
		}
	}
	return "", false
}

func splitFrontmatter(data []byte) (map[string]any, []byte, bool, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return nil, data, false, nil
	}
	// Require a line break after the opening fence.
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return nil, data, false, nil
	}
	first := strings.TrimSpace(s[:nl])
	if first != "---" {
		return nil, data, false, nil
	}
	rest := s[nl+1:]
	lines := strings.SplitAfter(rest, "\n")
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			fmText := rest[:offset]
			body := rest[offset+len(line):]
			var fm map[string]any
			if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
				return nil, data, true, fmt.Errorf("parse frontmatter: %w", err)
			}
			if fm == nil {
				fm = map[string]any{}
			}
			return fm, []byte(body), true, nil
		}
		offset += len(line)
	}
	return nil, data, true, fmt.Errorf("unterminated frontmatter")
}

func renderFrontmatter(fm map[string]any, body []byte) ([]byte, error) {
	raw, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(raw)
	b.WriteString("---\n")
	b.Write(body)
	return b.Bytes(), nil
}

func toStringList(v any) ([]string, bool) {
	switch x := v.(type) {
	case string:
		var out []string
		for _, p := range strings.Split(x, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out, true
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	case []string:
		return x, true
	}
	return nil, false
}

// Agent conversion is a registry of dialect pairs, mirroring commands.
type AgentConverter func(data []byte, from, to, filename string) ([]byte, []string, error)

var agentFormats = map[string]bool{
	AgentClaude:   true,
	AgentOpenCode: true,
}

var agentConverters = map[string]AgentConverter{
	AgentClaude + ">" + AgentOpenCode: agentToOpenCode,
	AgentOpenCode + ">" + AgentClaude: agentToClaude,
}

func RegisterAgentFormat(name string) {
	if strings.TrimSpace(name) == "" {
		panic("agent format needs a name")
	}
	agentFormats[name] = true
}

func RegisterAgentConverter(from, to string, fn AgentConverter) {
	if fn == nil || from == "" || to == "" {
		panic("agent converter needs from, to and implementation")
	}
	RegisterAgentFormat(from)
	RegisterAgentFormat(to)
	agentConverters[from+">"+to] = fn
}

func agentFormatKnown(f string) bool {
	return agentFormats[f]
}

func agentFormatOf(harness string) string {
	if h, ok := ResolveHarness(harness); ok {
		return h.Agents.Format
	}
	return ""
}

// ConvertAgent rewrites agent frontmatter for a different harness.
func ConvertAgent(data []byte, from, to string, filename string) ([]byte, []string, error) {
	fromFormat, toFormat := agentFormatOf(from), agentFormatOf(to)
	if fromFormat == "" || toFormat == "" || fromFormat == toFormat {
		return data, nil, nil
	}
	if fn, ok := agentConverters[fromFormat+">"+toFormat]; ok {
		return fn(data, fromFormat, toFormat, filename)
	}
	return data, []string{fmt.Sprintf("agent %s: no converter from %s to %s; copied verbatim", filename, fromFormat, toFormat)}, nil
}

func dropAgentFields(fm map[string]any, warnings []string, to, filename string, keys ...string) []string {
	for _, k := range keys {
		if _, exists := fm[k]; exists {
			warnings = append(warnings, fmt.Sprintf("agent %s: frontmatter field %q is not supported by %s and was dropped", filename, k, to))
			delete(fm, k)
		}
	}
	return warnings
}

func agentToOpenCode(data []byte, fromFormat, _, filename string) ([]byte, []string, error) {
	fm, body, ok, err := splitFrontmatter(data)
	if err != nil {
		return nil, nil, err
	}
	if !ok || len(fm) == 0 {
		return data, nil, nil
	}
	var warnings []string
	if v, ok := fm["tools"]; ok {
		names, isList := toStringList(v)
		if isList {
			obj := map[string]any{}
			for _, n := range names {
				c, ok := canonicalTool(n)
				if !ok {
					warnings = append(warnings, fmt.Sprintf("agent %s: unknown tool %q", filename, n))
					obj[strings.ToLower(n)] = true
					continue
				}
				obj[strings.ToLower(c)] = true
			}
			fm["tools"] = obj
		}
	}
	if fromFormat != AgentOpenCode {
		// Claude and Qwen agent files describe subagents; do not leak a
		// missing/legacy mode into OpenCode, where new agents default to primary.
		fm["mode"] = "subagent"
	} else if _, ok := fm["mode"]; !ok {
		fm["mode"] = "subagent"
	}
	warnings = dropAgentFields(fm, warnings, AgentOpenCode, filename,
		"name", "permissionMode", "approvalMode", "maxTurns", "disallowedTools", "mcpServers", "hooks")
	out, err := renderFrontmatter(fm, body)
	return out, warnings, err
}

func agentToClaude(data []byte, _, _, filename string) ([]byte, []string, error) {
	fm, body, ok, err := splitFrontmatter(data)
	if err != nil {
		return nil, nil, err
	}
	if !ok || len(fm) == 0 {
		return data, nil, nil
	}
	var warnings []string
	if _, ok := fm["name"]; !ok {
		name := strings.TrimSuffix(filepath.Base(filename), ".md")
		if name != "" {
			fm["name"] = name
		}
	}
	if v, ok := fm["tools"]; ok {
		if m, isMap := v.(map[string]any); isMap {
			var names []string
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				on, _ := m[k].(bool)
				if !on {
					continue
				}
				if c, ok := canonicalTool(k); ok {
					names = append(names, c)
				} else {
					warnings = append(warnings, fmt.Sprintf("agent %s: unknown tool %q", filename, k))
					names = append(names, k)
				}
			}
			fm["tools"] = strings.Join(names, ", ")
		}
	}
	warnings = dropAgentFields(fm, warnings, AgentClaude, filename,
		"mode", "permissions", "hidden", "steps", "temperature", "top_p", "request", "disabled", "subtask")
	out, err := renderFrontmatter(fm, body)
	return out, warnings, err
}
