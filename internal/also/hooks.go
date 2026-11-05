package also

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// HookSet maps harness event names to matcher groups, kept as raw JSON so
// fields also does not understand survive translation where event names match.
type HookSet map[string][]json.RawMessage

func hookEventsFor(h *Harness) map[string]bool {
	return h.HookEvents()
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

	for _, cfg := range h.Hooks.Configs {
		data, _, ok := find(cfg)
		if !ok {
			continue
		}
		if strings.HasSuffix(strings.ToLower(cfg), ".toml") {
			root, err := parseTOMLMap(data)
			if err != nil {
				return nil, warnings, fmt.Errorf("parse %s hooks: %w", cfg, err)
			}
			raw, ok := root["hooks"]
			if !ok {
				continue
			}
			b, _ := json.Marshal(raw)
			hs, w, err := hooksFromJSONConfig(b, "")
			if err != nil {
				return nil, warnings, err
			}
			merge(hs, w)
			continue
		}
		hs, w, err := hooksFromJSONConfig(data, "hooks")
		if err != nil {
			return nil, warnings, err
		}
		merge(hs, w)
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
	if len(target.Hooks.Configs) == 0 {
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
	return t.Harness.HooksTarget(t)
}
