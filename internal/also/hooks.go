package also

import (
	"encoding/json"
	"fmt"
	"sort"
)

// HookSet maps harness event names to matcher groups, kept as raw JSON so
// fields also does not understand survive translation where event names match.
type HookSet map[string][]json.RawMessage

var claudeHookEvents = []string{
	"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest",
	"UserPromptSubmit", "Notification", "Stop", "SubagentStop", "SubagentStart",
	"PreCompact", "SessionStart", "SessionEnd", "Setup",
}

var codexHookEvents = []string{
	"PreToolUse", "PermissionRequest", "PostToolUse", "PreCompact", "PostCompact",
	"UserPromptSubmit", "SubagentStop", "SubagentStart", "Stop", "SessionStart",
	"SessionEnd", "PreModelRequest", "PostModelRequest", "Interrupt",
}

func hookEventsFor(h *Harness) map[string]bool {
	out := map[string]bool{}
	var list []string
	switch h.HooksKind {
	case "claude", "qwen":
		list = claudeHookEvents
		if h.HooksKind == "qwen" {
			// Qwen also documents Gemini-style hook names in some versions.
			list = append(append([]string{}, claudeHookEvents...),
				"BeforeTool", "AfterTool", "BeforeAgent", "AfterAgent",
				"BeforeModel", "AfterModel", "BeforeToolSelection", "PreCompress")
		}
	case "codex":
		list = codexHookEvents
	default:
		return out
	}
	for _, e := range list {
		out[e] = true
	}
	return out
}

func ExtractBundleHooks(h *Harness, files map[string]*SnapshotFile, roots []string) (HookSet, []string, error) {
	out := HookSet{}
	var warnings []string
	find := func(candidates ...string) ([]byte, string, bool) {
		for _, root := range roots {
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
	merge := func(hs HookSet, w []string) {
		warnings = append(warnings, w...)
		for ev, groups := range hs {
			out[ev] = append(out[ev], groups...)
		}
	}

	switch h.HooksKind {
	case "claude", "qwen":
		if data, _, ok := find("settings.json"); ok {
			hs, w, err := hooksFromJSONConfig(data, "hooks")
			if err != nil {
				return nil, warnings, err
			}
			merge(hs, w)
		}
	case "codex":
		if data, _, ok := find("hooks.json"); ok {
			hs, w, err := hooksFromJSONConfig(data, "hooks")
			if err != nil {
				return nil, warnings, err
			}
			merge(hs, w)
		}
		if data, _, ok := find("config.toml"); ok {
			root, err := parseTOMLMap(data)
			if err != nil {
				return nil, warnings, fmt.Errorf("parse codex config.toml hooks: %w", err)
			}
			if raw, ok := root["hooks"]; ok {
				b, _ := json.Marshal(raw)
				hs, w, err := hooksFromJSONConfig(b, "")
				if err != nil {
					return nil, warnings, err
				}
				merge(hs, w)
			}
		}
	}
	return out, warnings, nil
}

func hooksFromJSONConfig(data []byte, rootKey string) (HookSet, []string, error) {
	var warnings []string
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, nil, err
	}
	raw := data
	if rootKey != "" {
		r, ok := doc.RawAt(rootKey)
		if !ok {
			return HookSet{}, nil, nil
		}
		raw = r
	}
	var m map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// Some configs use a non-array value; skip rather than corrupt.
		return nil, []string{"unsupported hooks shape; skipped"}, nil
	}
	return HookSet(m), warnings, nil
}

// RenderHooks filters and translates hook events for the target harness.
func RenderHooks(target *Harness, source *Harness, hs HookSet) (HookSet, []string) {
	out := HookSet{}
	var warnings []string
	if len(hs) == 0 {
		return out, warnings
	}
	if source != nil && source.ID == target.ID {
		// Native identity: never filter the source harness's own events.
		for ev, groups := range hs {
			out[ev] = append(out[ev], groups...)
		}
		return out, warnings
	}
	if target.HooksKind == "none" {
		return out, []string{"hooks are not declarative in this harness; hook definitions were skipped"}
	}
	supported := hookEventsFor(target)
	events := make([]string, 0, len(hs))
	for ev := range hs {
		events = append(events, ev)
	}
	sort.Strings(events)
	for _, ev := range events {
		if supported[ev] {
			out[ev] = append(out[ev], hs[ev]...)
		} else {
			warnings = append(warnings, fmt.Sprintf("hook event %s is not supported by %s; skipped", ev, target.Display))
		}
	}
	if source != nil && source.ID != target.ID && len(out) > 0 {
		warnings = append(warnings, "hook command bodies are copied verbatim; paths and harness-specific variables may need adjustment")
	}
	return out, warnings
}

func hooksConfigPath(t Target) (string, string) {
	switch t.Harness.HooksKind {
	case "claude", "qwen":
		return t.Settings, "hooks"
	case "codex":
		return t.HooksJSON, "hooks"
	}
	return "", ""
}
