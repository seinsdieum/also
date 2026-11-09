package also

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func testHarness(t *testing.T, id string) *Harness {
	t.Helper()
	h, ok := ResolveHarness(id)
	if !ok {
		t.Fatalf("unknown harness %q", id)
	}
	return h
}

func anyStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", v)
	}
	out := make([]string, 0, len(list))
	for _, raw := range list {
		s, ok := raw.(string)
		if !ok {
			t.Fatalf("array entry is not a string: %#v", raw)
		}
		out = append(out, s)
	}
	return out
}

func assertStringSet(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	got = append([]string{}, got...)
	want = append([]string{}, want...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func claudePermissions(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	perms, ok := out["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions = %#v, want a map", out["permissions"])
	}
	return perms
}

func openCodePermission(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	perms, ok := out["permission"].(map[string]any)
	if !ok {
		t.Fatalf("permission = %#v, want a map", out["permission"])
	}
	return perms
}

func warningsText(warnings []string) string { return strings.Join(warnings, "\n") }

func TestTranslateOpenCodeV1PermissionsToClaude(t *testing.T) {
	out, warnings := translateClaudeLikeSettings(map[string]any{
		"permission": map[string]any{"bash": "deny", "edit": "ask", "webfetch": "allow"},
	}, testHarness(t, "claude"), true)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	perms := claudePermissions(t, out)
	assertStringSet(t, "deny", anyStrings(t, perms["deny"]), "Bash")
	assertStringSet(t, "ask", anyStrings(t, perms["ask"]), "Edit", "NotebookEdit", "Write")
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]), "WebFetch")
}

func TestTranslateOpenCodeScopedRulesToClaudeFailClosed(t *testing.T) {
	out, warnings := translateClaudeLikeSettings(map[string]any{
		"permission": map[string]any{
			"bash":       map[string]any{"git push": "deny"},
			"edit":       map[string]any{"src/**": "ask"},
			"read":       map[string]any{"secret": "allow"},
			"frobnicate": "deny",
			"websearch":  "permit", // unsupported effect
		},
	}, testHarness(t, "claude"), true)
	perms := claudePermissions(t, out)
	assertStringSet(t, "deny", anyStrings(t, perms["deny"]), "Bash")
	assertStringSet(t, "ask", anyStrings(t, perms["ask"]), "Edit", "NotebookEdit", "Write")
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]))
	text := warningsText(warnings)
	if strings.Contains(text, "was applied") {
		t.Fatalf("translator still claims the value was applied:\n%s", text)
	}
	for _, want := range []string{
		`is scoped; Claude Code cannot express the scope, so the translation covers the whole Bash tool`,
		`is scoped; Claude Code cannot express the scope, so the translation covers the whole Edit, Write, NotebookEdit tools`,
		`cannot express a scoped allow safely`,
		`permission action "frobnicate" has no Claude Code equivalent`,
		`permission rule "websearch" has an unsupported effect "permit"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
}

func TestTranslateOpenCodeV2PermissionsToClaude(t *testing.T) {
	out, warnings := translateClaudeLikeSettings(map[string]any{
		"permissions": []any{
			// V2 spells the actions shell/subagent; the Claude target still
			// receives Bash/Task.
			map[string]any{"action": "shell", "effect": "deny"},
			map[string]any{"action": "edit", "resource": "src/**", "effect": "ask"},
			map[string]any{"action": "subagent", "effect": "allow"},
			map[string]any{"action": "webfetch", "resource": "https://example.com", "effect": "deny"},
		},
	}, testHarness(t, "claude"), true)
	perms := claudePermissions(t, out)
	assertStringSet(t, "deny", anyStrings(t, perms["deny"]), "Bash", "WebFetch")
	assertStringSet(t, "ask", anyStrings(t, perms["ask"]), "Edit", "NotebookEdit", "Write")
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]), "Task")
	text := warningsText(warnings)
	if strings.Contains(text, "was applied") {
		t.Fatalf("translator still claims the value was applied:\n%s", text)
	}
	for _, want := range []string{
		`is scoped; Claude Code cannot express the scope, so the translation covers the whole Edit, Write, NotebookEdit tools`,
		`is scoped; Claude Code cannot express the scope, so the translation covers the whole WebFetch tool`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing scope warning %q:\n%s", want, text)
		}
	}
}

func TestTranslateClaudePermissionsToOpenCode(t *testing.T) {
	out, warnings := translateOpenCodeSettings(map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Task"},
			"ask":   []any{"Edit"},
			"deny":  []any{"Bash"},
		},
	}, testHarness(t, "opencode"))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	perms := openCodePermission(t, out)
	want := map[string]any{"read": "allow", "task": "allow", "edit": "ask", "bash": "deny"}
	if !reflect.DeepEqual(perms, want) {
		t.Fatalf("permission = %v, want the v1 keys %v", perms, want)
	}
	for _, forbidden := range []string{"shell", "subagent"} {
		if _, ok := perms[forbidden]; ok {
			t.Fatalf("OpenCode target used the v2 action %q: %v", forbidden, perms)
		}
	}
}

func TestOpenCodeTargetCanonicalisesV2SourceActions(t *testing.T) {
	out, _ := translateOpenCodeSettings(map[string]any{
		"permissions": []any{
			map[string]any{"action": "shell", "effect": "deny"},
			map[string]any{"action": "subagent", "effect": "ask"},
		},
	}, testHarness(t, "opencode"))
	perms := openCodePermission(t, out)
	want := map[string]any{"bash": "deny", "task": "ask"}
	if !reflect.DeepEqual(perms, want) {
		t.Fatalf("permission = %v, want %v", perms, want)
	}
	for _, forbidden := range []string{"shell", "subagent"} {
		if _, ok := perms[forbidden]; ok {
			t.Fatalf("OpenCode target used the v2 action %q: %v", forbidden, perms)
		}
	}
}

func TestTranslateClaudeScopedRulesToOpenCodeFailClosed(t *testing.T) {
	out, warnings := translateOpenCodeSettings(map[string]any{
		"permissions": map[string]any{
			"deny":  []any{"Bash(git push:*)"},
			"allow": []any{"Read(secret)"},
			"ask":   []any{"Custom"},
		},
	}, testHarness(t, "opencode"))
	perms := openCodePermission(t, out)
	if perms["bash"] != "deny" {
		t.Fatalf("scoped deny was not broadened to bash deny: %v", perms)
	}
	if _, ok := perms["read"]; ok {
		t.Fatalf("scoped allow was kept: %v", perms)
	}
	text := warningsText(warnings)
	if strings.Contains(text, "was applied") {
		t.Fatalf("translator still claims the value was applied:\n%s", text)
	}
	for _, want := range []string{
		`is scoped; OpenCode cannot express the scope, so the translation covers the whole "bash" action`,
		`cannot express a scoped allow safely`,
		`permission tool "Custom" has no OpenCode equivalent`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
}

func TestTranslatePermissionsNeverWeaken(t *testing.T) {
	// deny wins over allow; ask never degenerates into allow.
	out, _ := translateOpenCodeSettings(map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash", "Edit"},
			"ask":   []any{"Edit"},
			"deny":  []any{"Bash"},
		},
	}, testHarness(t, "opencode"))
	perms := openCodePermission(t, out)
	if perms["bash"] != "deny" {
		t.Fatalf("allow+deny for Bash = %v, want deny", perms["bash"])
	}
	if perms["edit"] != "ask" {
		t.Fatalf("allow+ask for Edit = %v, want ask", perms["edit"])
	}

	// The same merge in the Claude direction.
	out, _ = translateClaudeLikeSettings(map[string]any{
		"permission": map[string]any{"bash": "allow"},
		"permissions": []any{
			map[string]any{"action": "bash", "effect": "deny"},
		},
	}, testHarness(t, "claude"), true)
	permsClaude := claudePermissions(t, out)
	assertStringSet(t, "deny", anyStrings(t, permsClaude["deny"]), "Bash")
	assertStringSet(t, "allow", anyStrings(t, permsClaude["allow"]))
}

func TestTranslateClaudeQwenPermissions(t *testing.T) {
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"WebFetch"},
			"ask":   []any{"Read"},
			"deny":  []any{"Bash"},
		},
	}
	toQwen, warnings := translateClaudeLikeSettings(settings, testHarness(t, "qwen"), false)
	if len(warnings) != 0 {
		t.Fatalf("unexpected Qwen warnings: %v", warnings)
	}
	perms := claudePermissions(t, toQwen)
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]), "WebFetch")
	assertStringSet(t, "ask", anyStrings(t, perms["ask"]), "Read")
	assertStringSet(t, "deny", anyStrings(t, perms["deny"]), "Bash")

	backToClaude, warnings := translateClaudeLikeSettings(settings, testHarness(t, "claude"), true)
	if len(warnings) != 0 {
		t.Fatalf("unexpected Claude warnings: %v", warnings)
	}
	perms = claudePermissions(t, backToClaude)
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]), "WebFetch")
	assertStringSet(t, "ask", anyStrings(t, perms["ask"]), "Read")
	assertStringSet(t, "deny", anyStrings(t, perms["deny"]), "Bash")
}

func TestTranslateCodexSettingsArePreserved(t *testing.T) {
	settings := map[string]any{
		"theme":       "dark",
		"permissions": map[string]any{"deny": []any{"Bash"}},
		"model":       "opus",
	}
	out, warnings := translateCodexSettings(settings, testHarness(t, "codex"))
	if len(out) != 0 {
		t.Fatalf("Codex received settings: %v", out)
	}
	text := warningsText(warnings)
	for _, want := range []string{
		"preserved in the package",
		"Codex CLI has no mergeable",
		"model, permissions, theme",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
}

func TestTranslateThemeAndRemainingKeys(t *testing.T) {
	settings := map[string]any{
		"theme":         "dark",
		"model":         "opus",
		"statusLine":    map[string]any{"type": "command"},
		"small_model":   "haiku",
		"default_agent": "build",
		"keybinds":      map[string]any{"leader": "space"},
		"experimental":  true,
	}

	toClaude, warnings := translateClaudeLikeSettings(settings, testHarness(t, "claude"), true)
	if toClaude["theme"] != "dark" {
		t.Fatalf("theme was not transferred to Claude: %v", toClaude)
	}
	text := warningsText(warnings)
	if !strings.Contains(text, `theme "dark" was translated for Claude Code`) || !strings.Contains(text, "may not exist") {
		t.Fatalf("missing theme warning:\n%s", text)
	}
	if strings.Contains(text, "was applied") {
		t.Fatalf("translator still claims the theme was applied:\n%s", text)
	}
	for _, key := range []string{"model", "statusLine", "small_model", "default_agent", "keybinds", "experimental"} {
		if !strings.Contains(text, `setting "`+key+`" has no Claude Code equivalent`) {
			t.Fatalf("missing preserved-key warning for %q:\n%s", key, text)
		}
		if _, ok := toClaude[key]; ok {
			t.Fatalf("%q was installed without translation", key)
		}
	}

	toOpenCode, warnings := translateOpenCodeSettings(settings, testHarness(t, "opencode"))
	if toOpenCode["theme"] != "dark" {
		t.Fatalf("theme was not transferred to OpenCode: %v", toOpenCode)
	}
	if !strings.Contains(warningsText(warnings), `theme "dark" was translated for OpenCode`) {
		t.Fatalf("missing OpenCode theme warning: %v", warnings)
	}

	toQwen, warnings := translateClaudeLikeSettings(settings, testHarness(t, "qwen"), false)
	if _, ok := toQwen["theme"]; ok {
		t.Fatalf("theme was transferred to Qwen: %v", toQwen)
	}
	if !strings.Contains(warningsText(warnings), `setting "theme" has no Qwen Code equivalent`) {
		t.Fatalf("missing Qwen theme warning: %v", warnings)
	}
}

func TestTranslateUnknownPermissionSubkeyWarns(t *testing.T) {
	out, warnings := translateClaudeLikeSettings(map[string]any{
		"permissions": map[string]any{
			"allow":   []any{"Read"},
			"execute": "deny",
		},
	}, testHarness(t, "claude"), true)
	perms := claudePermissions(t, out)
	assertStringSet(t, "allow", anyStrings(t, perms["allow"]), "Read")
	if _, ok := perms["execute"]; ok {
		t.Fatalf("unknown permission subkey was translated: %v", perms)
	}
	if !strings.Contains(warningsText(warnings), `permission key "execute" has no Claude Code equivalent`) {
		t.Fatalf("missing unknown-subkey warning: %v", warnings)
	}
}

func TestPermissionActionTableRoundTrips(t *testing.T) {
	want := map[string]struct {
		aliases []string
		tools   []string
	}{
		"edit":      {[]string{"edit", "write", "patch"}, []string{"Edit", "Write", "NotebookEdit"}},
		"bash":      {[]string{"bash", "shell"}, []string{"Bash"}},
		"webfetch":  {[]string{"webfetch"}, []string{"WebFetch"}},
		"websearch": {[]string{"websearch"}, []string{"WebSearch"}},
		"task":      {[]string{"task", "subagent"}, []string{"Task"}},
		"read":      {[]string{"read"}, []string{"Read"}},
		"grep":      {[]string{"grep"}, []string{"Grep"}},
		"glob":      {[]string{"glob"}, []string{"Glob"}},
		"list":      {[]string{"list"}, []string{"List"}},
		"todowrite": {[]string{"todowrite", "todo"}, []string{"TodoWrite"}},
	}
	if len(permissionActions) != len(want) {
		t.Fatalf("permissionActions has %d entries, want %d", len(permissionActions), len(want))
	}
	for _, action := range permissionActions {
		expected, ok := want[action.V1]
		if !ok {
			t.Fatalf("unexpected permission action %q", action.V1)
		}
		assertStringSet(t, action.V1+" aliases", action.Aliases, expected.aliases...)
		assertStringSet(t, action.V1+" tools", action.Tools, expected.tools...)
		for _, alias := range action.Aliases {
			if got, ok := openCodeV1Action(alias); !ok || got != action.V1 {
				t.Fatalf("alias %q round-trips to %q, want %q", alias, got, action.V1)
			}
		}
		for _, tool := range action.Tools {
			if got, ok := actionForClaudeTool(tool); !ok || got != action.V1 {
				t.Fatalf("tool %q round-trips to %q, want %q", tool, got, action.V1)
			}
		}
		assertStringSet(t, action.V1+" claude tools", claudeToolsForAction(action.V1), expected.tools...)
	}
	for _, special := range []string{"*"} {
		if got, ok := openCodeV1Action(special); !ok || got != special {
			t.Fatalf("openCodeV1Action(%q) = %q,%v", special, got, ok)
		}
		if got, ok := actionForClaudeTool(special); !ok || got != special {
			t.Fatalf("actionForClaudeTool(%q) = %q,%v", special, got, ok)
		}
	}
}

func TestRetentionDropsOnlyOldAllowGrants(t *testing.T) {
	cases := []struct {
		name string
		edit ConfigEdit
		keep bool
	}{
		{"Claude allow array", ConfigEdit{Kind: "json-append", Path: []string{"permissions", "allow"}, Value: json.RawMessage(`"Bash"`)}, false},
		{"Claude deny array", ConfigEdit{Kind: "json-append", Path: []string{"permissions", "deny"}, Value: json.RawMessage(`"Bash"`)}, true},
		{"Claude ask array", ConfigEdit{Kind: "json-append", Path: []string{"permissions", "ask"}, Value: json.RawMessage(`"Bash"`)}, true},
		{"OpenCode allow leaf", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash"}, Value: json.RawMessage(`"allow"`)}, false},
		{"OpenCode scoped allow leaf", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash", "git push"}, Value: json.RawMessage(`"allow"`)}, false},
		{"OpenCode deny leaf", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash"}, Value: json.RawMessage(`"deny"`)}, true},
		{"OpenCode ask leaf", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash"}, Value: json.RawMessage(`"ask"`)}, true},
		{"OpenCode scoped deny leaf", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash", "git push"}, Value: json.RawMessage(`"deny"`)}, true},
		{"unsupported permission shape", ConfigEdit{Kind: "json-set", Path: []string{"permission", "bash"}, Value: json.RawMessage(`{"git push":"allow"}`)}, true},
		{"unrelated setting", ConfigEdit{Kind: "json-set", Path: []string{"theme"}, Value: json.RawMessage(`"dark"`)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepOldSettingEdit(tc.edit); got != tc.keep {
				t.Fatalf("keepOldSettingEdit(%v) = %v, want %v", tc.edit.Path, got, tc.keep)
			}
		})
	}
}

func TestTranslateGlobalPermissionString(t *testing.T) {
	for _, target := range []string{"claude", "qwen"} {
		t.Run(target, func(t *testing.T) {
			out, warnings := translateClaudeLikeSettings(map[string]any{
				"permission": "deny",
			}, testHarness(t, target), target == "claude")
			perms := claudePermissions(t, out)
			deny := anyStrings(t, perms["deny"])
			for _, tool := range []string{
				"Bash", "Edit", "Write", "NotebookEdit", "Read", "Grep", "Glob",
				"WebFetch", "WebSearch", "Task", "TodoWrite", "List",
			} {
				found := false
				for _, got := range deny {
					if got == tool {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("global deny is missing %s: %v", tool, deny)
				}
			}
			assertStringSet(t, "allow", anyStrings(t, perms["allow"]))
			assertStringSet(t, "ask", anyStrings(t, perms["ask"]))
			if strings.Contains(warningsText(warnings), "was applied") {
				t.Fatalf("translator still claims the value was applied: %v", warnings)
			}
		})
	}
}
