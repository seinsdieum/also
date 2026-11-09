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

// permissionAction is the single source of truth for the OpenCode permission
// action <-> Claude tool mapping. V1 is the OpenCode v1 permission-map key; the
// aliases cover v2 and source spellings. Every alias and tool must round-trip,
// which init verifies because this is compiled-in policy.
type permissionAction struct {
	V1      string
	Aliases []string
	Tools   []string
}

var permissionActions = []permissionAction{
	{V1: "edit", Aliases: []string{"edit", "write", "patch"}, Tools: []string{"Edit", "Write", "NotebookEdit"}},
	{V1: "bash", Aliases: []string{"bash", "shell"}, Tools: []string{"Bash"}},
	{V1: "webfetch", Aliases: []string{"webfetch"}, Tools: []string{"WebFetch"}},
	{V1: "websearch", Aliases: []string{"websearch"}, Tools: []string{"WebSearch"}},
	{V1: "task", Aliases: []string{"task", "subagent"}, Tools: []string{"Task"}},
	{V1: "read", Aliases: []string{"read"}, Tools: []string{"Read"}},
	{V1: "grep", Aliases: []string{"grep"}, Tools: []string{"Grep"}},
	{V1: "glob", Aliases: []string{"glob"}, Tools: []string{"Glob"}},
	{V1: "list", Aliases: []string{"list"}, Tools: []string{"List"}},
	{V1: "todowrite", Aliases: []string{"todowrite", "todo"}, Tools: []string{"TodoWrite"}},
}

func init() {
	if err := validatePermissionActions(); err != nil {
		panic(err)
	}
}

func validatePermissionActions() error {
	aliases := map[string]string{}
	tools := map[string]string{}
	for _, a := range permissionActions {
		if a.V1 == "" || len(a.Aliases) == 0 || len(a.Tools) == 0 {
			return fmt.Errorf("permission action %q is incomplete", a.V1)
		}
		for _, alias := range a.Aliases {
			if prev, ok := aliases[alias]; ok && prev != a.V1 {
				return fmt.Errorf("permission alias %q maps to both %q and %q", alias, prev, a.V1)
			}
			aliases[alias] = a.V1
			if got, ok := openCodeV1Action(alias); !ok || got != a.V1 {
				return fmt.Errorf("permission alias %q does not round-trip to %q", alias, a.V1)
			}
		}
		for _, tool := range a.Tools {
			name, ok := canonicalTool(tool)
			if !ok || name != tool {
				return fmt.Errorf("permission action %q uses unknown Claude tool %q", a.V1, tool)
			}
			if prev, ok := tools[name]; ok && prev != a.V1 {
				return fmt.Errorf("Claude tool %q maps to both %q and %q", name, prev, a.V1)
			}
			tools[name] = a.V1
			if got, ok := actionForClaudeTool(name); !ok || got != a.V1 {
				return fmt.Errorf("Claude tool %q does not round-trip to %q", name, a.V1)
			}
		}
	}
	return nil
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
// target is the human display name of the destination harness, so warnings name
// Qwen as Qwen instead of always saying Claude.
type AgentConverter func(data []byte, from, to, target, filename string) ([]byte, []string, error)

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
		target := to
		if h, ok := ResolveHarness(to); ok {
			target = h.Display
		}
		return fn(data, fromFormat, toFormat, target, filename)
	}
	return data, []string{fmt.Sprintf("agent %s: no converter from %s to %s; copied verbatim", filename, fromFormat, toFormat)}, nil
}

func isClaudeModel(m string) bool {
	m = strings.TrimSpace(m)
	if m == "" {
		return true
	}
	switch strings.ToLower(m) {
	case "sonnet", "opus", "haiku", "inherit":
		return true
	}
	return strings.HasPrefix(strings.ToLower(m), "claude-")
}

func isOpenCodeModel(m string) bool {
	m = strings.TrimSpace(m)
	if m == "" {
		return true
	}
	parts := strings.Split(m, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func permissionRestricts(v any) bool {
	switch x := v.(type) {
	case string:
		effect := strings.ToLower(strings.TrimSpace(x))
		// "ask" has no Claude agent equivalent. Removing the tool is the
		// fail-closed mapping; dropping the rule would turn ask into allow.
		return effect == "deny" || effect == "ask"
	case map[string]any:
		// A per-command map cannot be expressed by Claude's agent
		// frontmatter. Any restriction means the safe mapping is to remove
		// the whole tool rather than silently allow the restricted command.
		for _, raw := range x {
			if permissionRestricts(raw) {
				return true
			}
		}
	}
	return false
}

// claudeToolsForAction maps one OpenCode permission action, v1 or v2, to the
// Claude tools it covers. An unknown action is reported to the caller.
func claudeToolsForAction(action string) []string {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "*" {
		all := make([]string, 0, len(toolNames))
		for _, name := range toolNames {
			all = append(all, name)
		}
		return all
	}
	for _, a := range permissionActions {
		for _, alias := range a.Aliases {
			if alias == action {
				return a.Tools
			}
		}
	}
	return nil
}

// actionForClaudeTool is the inverse mapping: a canonical Claude tool becomes
// the OpenCode v1 permission-map key.
func actionForClaudeTool(tool string) (string, bool) {
	tool = strings.TrimSpace(tool)
	if tool == "*" {
		return "*", true
	}
	c, ok := canonicalTool(tool)
	if !ok {
		return "", false
	}
	for _, a := range permissionActions {
		for _, name := range a.Tools {
			if name == c {
				return a.V1, true
			}
		}
	}
	return "", false
}

// openCodeV1Action canonicalizes an OpenCode action to the v1 permission-map
// key. A v2 source uses shell/subagent, which v1 spells bash/task.
func openCodeV1Action(action string) (string, bool) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "*" {
		return "*", true
	}
	for _, a := range permissionActions {
		for _, alias := range a.Aliases {
			if alias == action {
				return a.V1, true
			}
		}
	}
	return "", false
}

func toolSet(v any) map[string]bool {
	out := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if c, ok := canonicalTool(name); ok {
			out[c] = true
		} else {
			out[name] = true
		}
	}
	switch x := v.(type) {
	case string:
		for _, part := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
			add(part)
		}
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				add(s)
			}
		}
	case []string:
		for _, s := range x {
			add(s)
		}
	case map[string]any:
		for k, raw := range x {
			if on, _ := raw.(bool); on {
				add(k)
			}
		}
	}
	return out
}

func claudeToolsString(set map[string]bool) string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func dropTools(set map[string]bool, names ...string) {
	for _, name := range names {
		if c, ok := canonicalTool(name); ok {
			delete(set, c)
		} else {
			delete(set, name)
		}
	}
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

func agentToOpenCode(data []byte, fromFormat, _, target, filename string) ([]byte, []string, error) {
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
	if m, ok := fm["model"].(string); ok && m != "" && !isOpenCodeModel(m) {
		warnings = append(warnings, fmt.Sprintf("agent %s: model %q is not an OpenCode provider/model id and was dropped; set an OpenCode-native model in the package", filename, m))
		delete(fm, "model")
	}
	tools := map[string]any{}
	if m, ok := fm["tools"].(map[string]any); ok {
		for k, v := range m {
			tools[k] = v
		}
	}
	disableTool := func(name string) {
		if c, ok := canonicalTool(name); ok {
			tools[strings.ToLower(c)] = false
		} else {
			tools[strings.ToLower(name)] = false
		}
	}
	if mode, ok := fm["permissionMode"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "plan":
			disableTool("Edit")
			disableTool("Write")
			disableTool("NotebookEdit")
			warnings = append(warnings, fmt.Sprintf("agent %s: permissionMode=plan was mapped to disabled edit tools", filename))
		}
		delete(fm, "permissionMode")
	}
	if v, ok := fm["disallowedTools"]; ok {
		names, _ := toStringList(v)
		for _, name := range names {
			disableTool(name)
		}
		delete(fm, "disallowedTools")
	}
	if len(tools) > 0 {
		fm["tools"] = tools
	}
	warnings = dropAgentFields(fm, warnings, target, filename,
		"name", "approvalMode", "maxTurns", "mcpServers", "hooks")
	out, err := renderFrontmatter(fm, body)
	return out, warnings, err
}

func agentToClaude(data []byte, _, _, target, filename string) ([]byte, []string, error) {
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
	if m, ok := fm["model"].(string); ok && m != "" && !isClaudeModel(m) {
		warnings = append(warnings, fmt.Sprintf("agent %s: model %q is not a Claude model and was dropped; set a Claude-native model in the package", filename, m))
		delete(fm, "model")
	}
	hasTools := fm["tools"] != nil
	tools := toolSet(fm["tools"])
	var drops []string
	if perm, ok := fm["permission"].(map[string]any); ok {
		// Claude has no per-tool permission map. Any deny or ask becomes a
		// tool allowlist removal, which preserves the guardrail. Ignoring
		// "ask" here would silently turn it into "allow".
		for key, raw := range perm {
			if !permissionRestricts(raw) {
				continue
			}
			names := claudeToolsForAction(key)
			if len(names) == 0 {
				warnings = append(warnings, fmt.Sprintf("agent %s: permission %q has no %s equivalent and was dropped", filename, key, target))
				continue
			}
			drops = append(drops, names...)
		}
		warnings = append(warnings, fmt.Sprintf("agent %s: OpenCode permission map is not supported by %s; deny/ask rules removed the affected tools, while allow patterns cannot be expressed and were dropped", filename, target))
		delete(fm, "permission")
	}
	if rules, ok := fm["permissions"].([]any); ok {
		// OpenCode V2 uses an ordered permissions array. A restricted action
		// still has to survive the trip to a tool allowlist.
		for _, raw := range rules {
			rule, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			effect, _ := rule["effect"].(string)
			if !permissionRestricts(effect) {
				continue
			}
			action, _ := rule["action"].(string)
			names := claudeToolsForAction(action)
			if len(names) == 0 {
				warnings = append(warnings, fmt.Sprintf("agent %s: permission action %q has no %s equivalent and was dropped", filename, action, target))
				continue
			}
			if resource, _ := rule["resource"].(string); resource != "" && resource != "*" {
				warnings = append(warnings, fmt.Sprintf("agent %s: %s cannot scope %q to %q; the whole tool was removed", filename, target, action, resource))
			}
			drops = append(drops, names...)
		}
		warnings = append(warnings, fmt.Sprintf("agent %s: OpenCode permissions array is not supported by %s; deny/ask rules removed the affected tools, while allow rules cannot be expressed and were dropped", filename, target))
		delete(fm, "permissions")
	}
	if len(drops) > 0 && !hasTools {
		for _, name := range toolNames {
			tools[name] = true
		}
	}
	dropTools(tools, drops...)
	if hasTools || len(drops) > 0 {
		fm["tools"] = claudeToolsString(tools)
	}
	warnings = dropAgentFields(fm, warnings, target, filename,
		"mode", "hidden", "steps", "temperature", "top_p", "request", "disabled", "subtask")
	out, err := renderFrontmatter(fm, body)
	return out, warnings, err
}
