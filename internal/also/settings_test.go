package also

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHooksSameHarnessKeepsAllEvents(t *testing.T) {
	hs := HookSet{
		"Setup":              {[]byte(`{"hooks":[{"type":"command","command":"echo setup"}]}`)},
		"PostToolUseFailure": {[]byte(`{"hooks":[{"type":"command","command":"echo fail"}]}`)},
		"PermissionRequest":  {[]byte(`{"hooks":[{"type":"command","command":"echo perm"}]}`)},
	}
	out, warns := RenderHooks(MustHarness("claude"), MustHarness("claude"), hs)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(out) != 3 {
		t.Fatalf("same-harness hooks must be identity, got %d events", len(out))
	}
	// Unsupported events are filtered only cross-harness.
	out, _ = RenderHooks(MustHarness("codex"), MustHarness("claude"), hs)
	if _, ok := out["Setup"]; ok {
		t.Fatal("codex should not receive Setup")
	}
}

func TestExtractSettingsAndTargets(t *testing.T) {
	files := map[string]*SnapshotFile{
		"settings.json": {Data: []byte(`{"permissions":{"allow":["Bash(ls:*)"]},"statusLine":{"type":"command"},"model":"x"}`)},
	}
	got, _, err := ExtractBundleSettings(MustHarness("claude"), files, []string{""})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["model"]; ok {
		t.Fatal("non-portable settings key leaked")
	}
	if got["permissions"] == nil || got["statusLine"] == nil {
		t.Fatalf("missing settings: %+v", got)
	}
	if f := SettingsTargetFile(MustHarness("claude").TargetPath("/tmp/p")); !strings.HasSuffix(f, filepath.Join(".claude", "settings.json")) {
		t.Fatalf("bad claude settings target: %s", f)
	}
	if f := SettingsTargetFile(MustHarness("opencode").TargetPath("/tmp/p")); !strings.HasSuffix(f, "opencode.json") {
		t.Fatalf("bad opencode settings target: %s", f)
	}
}

func TestMCPRawSameHarness(t *testing.T) {
	raw := map[string]any{"command": "x", "disabled": true, "timeout": map[string]any{"startup": 10}}
	servers := map[string]MCPServer{"s": {Transport: "stdio", Command: []string{"x"}, Raw: raw}}
	out, warns := RenderMCP(MustHarness("claude"), MustHarness("claude"), servers)
	if len(warns) != 0 || len(out) != 1 {
		t.Fatalf("render: %v %v", out, warns)
	}
	v, ok := out[0].Value.(map[string]any)
	if !ok || v["disabled"] != true {
		t.Fatalf("native fields lost: %#v", out[0].Value)
	}
}
