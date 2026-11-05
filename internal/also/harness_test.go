package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectHarness(t *testing.T) {
	cases := map[string][]string{
		"claude":   {"CLAUDE.md"},
		"opencode": {"opencode.jsonc", ".opencode/x"},
		"codex":    {".codex/x", "config.toml"},
		"qwen":     {"QWEN.md"},
	}
	for id, files := range cases {
		dir := t.TempDir()
		for _, f := range files {
			p := filepath.Join(dir, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := DetectHarness(dir)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got != id {
			t.Fatalf("%s: got %s", id, got)
		}
	}
}

func TestDetectHarnessAmbiguous(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "QWEN.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DetectHarness(dir); err == nil {
		t.Fatal("expected ambiguity error")
	}
}

func TestTargets(t *testing.T) {
	project := t.TempDir()
	oc := MustHarness("opencode").TargetPath("project", project)
	if !strings.HasSuffix(oc.Skills, filepath.Join(".opencode", "skills")) {
		t.Fatalf("opencode skills: %s", oc.Skills)
	}
	if filepath.Base(oc.Instr) != "AGENTS.md" {
		t.Fatalf("opencode instructions: %s", oc.Instr)
	}
	cx := MustHarness("codex").TargetPath("project", project)
	if !strings.HasSuffix(cx.Skills, filepath.Join(".agents", "skills")) {
		t.Fatalf("codex skills: %s", cx.Skills)
	}
	cl := MustHarness("claude").TargetPath("project", project)
	if filepath.Base(cl.Instr) != "CLAUDE.md" {
		t.Fatalf("claude instructions: %s", cl.Instr)
	}
}

func TestConvertAgentClaudeToOpenCode(t *testing.T) {
	in := []byte("---\nname: critic\ndescription: finds bugs\ntools: Read, Grep\nmode: x\npermissionMode: plan\n---\nbody\n")
	out, warnings, err := ConvertAgent(in, "claude", "opencode", "critic.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "name:") || strings.Contains(s, "permissionMode") {
		t.Fatalf("unsupported fields not dropped:\n%s", s)
	}
	if !strings.Contains(s, "mode: subagent") {
		t.Fatalf("mode not forced:\n%s", s)
	}
	if !strings.Contains(s, "read: true") || !strings.Contains(s, "grep: true") {
		t.Fatalf("tools not converted:\n%s", s)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warnings")
	}
}

func TestConvertAgentOpenCodeToClaude(t *testing.T) {
	in := []byte("---\ndescription: d\nmode: subagent\ntools:\n  read: true\n  write: false\nhidden: true\n---\nbody\n")
	out, _, err := ConvertAgent(in, "opencode", "claude", "a.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "mode:") || strings.Contains(s, "hidden:") {
		t.Fatalf("unsupported fields not dropped:\n%s", s)
	}
	if !strings.Contains(s, "tools: Read") {
		t.Fatalf("tools not converted:\n%s", s)
	}
}
