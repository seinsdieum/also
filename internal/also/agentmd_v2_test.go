package also

import (
	"strings"
	"testing"
)

func TestConvertAgentAskFailsClosed(t *testing.T) {
	in := []byte("---\ndescription: d\npermission:\n  edit: ask\n  bash: allow\n---\nbody\n")
	out, warnings, err := ConvertAgent(in, "opencode", "claude", "dev.md")
	if err != nil {
		t.Fatal(err)
	}
	fm, _, ok, err := splitFrontmatter(out)
	if err != nil || !ok {
		t.Fatalf("converted frontmatter is invalid: %v\n%s", err, out)
	}
	tools, _ := fm["tools"].(string)
	for _, denied := range []string{"Edit", "Write", "NotebookEdit"} {
		for _, got := range strings.Split(tools, ", ") {
			if got == denied {
				t.Fatalf("ask was converted to allow: %s in tools=%q", denied, tools)
			}
		}
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "Claude Code") {
		t.Fatalf("warning does not name the target harness: %v", warnings)
	}
}

func TestConvertAgentV2PermissionsFailsClosed(t *testing.T) {
	in := []byte("---\ndescription: d\npermissions:\n  - action: shell\n    resource: \"*\"\n    effect: deny\n  - action: subagent\n    resource: reviewer\n    effect: ask\n---\nbody\n")
	out, warnings, err := ConvertAgent(in, "opencode", "claude", "dev.md")
	if err != nil {
		t.Fatal(err)
	}
	fm, _, ok, err := splitFrontmatter(out)
	if err != nil || !ok {
		t.Fatalf("converted frontmatter is invalid: %v\n%s", err, out)
	}
	tools, _ := fm["tools"].(string)
	if strings.Contains(tools, "Bash") || strings.Contains(tools, "Task") {
		t.Fatalf("V2 deny/ask was dropped: tools=%q", tools)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "cannot scope") {
		t.Fatalf("scoped permission warning is missing: %v", warnings)
	}
}

func TestConvertAgentWarnsWithQwenName(t *testing.T) {
	in := []byte("---\ndescription: d\nmode: subagent\ntemperature: 0.2\n---\nbody\n")
	_, warnings, err := ConvertAgent(in, "opencode", "qwen", "dev.md")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "Qwen Code") {
		t.Fatalf("Qwen warning named the wrong target: %v", warnings)
	}
	if strings.Contains(joined, "Claude") {
		t.Fatalf("Qwen warning still says Claude: %v", warnings)
	}
}

func TestOpenCodeSettingsAllowExperimental(t *testing.T) {
	keys := MustHarness("opencode").Settings.Keys
	found := false
	for _, k := range keys {
		if k == "experimental" {
			found = true
		}
	}
	if !found {
		t.Fatalf("experimental is not installable: %v", keys)
	}
}
