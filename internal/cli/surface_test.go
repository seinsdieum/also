package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI contract is deliberately small: four wedge commands, setup and
// inspection, and one maintenance command. Everything else is a flag or a
// hidden advanced command.
func TestRootSurfaceIsThirteenCommands(t *testing.T) {
	root := NewRoot()
	want := []string{
		"install", "push", "audit", "uninstall",
		"setup", "init", "connect", "adopt", "upgrade", "tag",
		"list", "show", "doctor",
	}
	visible := map[string]string{}
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		visible[c.Name()] = c.GroupID
	}
	if len(visible) != len(want) {
		t.Fatalf("visible commands = %d %v, want %d", len(visible), visible, len(want))
	}
	for i, name := range want {
		if _, ok := visible[name]; !ok {
			t.Fatalf("missing visible command %q: %v", name, visible)
		}
		if i < 4 && visible[name] != "core" {
			t.Fatalf("%q must be in the core group, got %q", name, visible[name])
		}
	}
	for _, removed := range []string{"search", "drift", "verify", "tree", "use", "prune", "repair", "update"} {
		for _, c := range root.Commands() {
			if c.Name() == removed {
				t.Fatalf("%q should be folded into another command", removed)
			}
		}
	}
}

func TestFoldedJSONShapesUseArrays(t *testing.T) {
	setupHome(t)
	out, err := runCLICapture(t, "list", "--harnesses", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var harnesses []any
	if err := json.Unmarshal([]byte(out), &harnesses); err != nil {
		t.Fatalf("list --harnesses --json is not JSON: %v\n%s", err, out)
	}
	if len(harnesses) == 0 {
		t.Fatal("list --harnesses --json returned no adapters")
	}
	out, err = runCLICapture(t, "list", "--tree", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tree []any
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatalf("empty tree is not a JSON array: %v\n%s", err, out)
	}
}

func TestInstallMaintainsManagedGitIgnore(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "also.json"), []byte(`{"schema":"also.infra/v1","name":"demo","harness":"claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("push", bundle); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# also:begin infra", ".claude/", "CLAUDE.md", "# also:begin you"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf(".gitignore is missing %q:\n%s", want, data)
		}
	}
}

func TestUninstallPrunesManagedGitIgnore(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "opencode", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "also.json"), []byte(`{"schema":"also.infra/v1","name":"demo","harness":"opencode"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "AGENTS.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("push", bundle); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	for _, h := range []string{"opencode", "claude"} {
		if err := runCLI("install", "demo", "for", h); err != nil {
			t.Fatal(err)
		}
	}
	ignore := func() string {
		data, err := os.ReadFile(filepath.Join(project, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if err := runCLI("uninstall", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	after := ignore()
	for _, gone := range []string{".claude/", "CLAUDE.md", ".mcp.json"} {
		if strings.Contains(after, gone) {
			t.Fatalf("uninstall left %q in .gitignore:\n%s", gone, after)
		}
	}
	for _, kept := range []string{".opencode/", "AGENTS.md", "opencode.json"} {
		if !strings.Contains(after, kept) {
			t.Fatalf("uninstall removed a still-needed entry %q:\n%s", kept, after)
		}
	}
	if err := runCLI("uninstall", "demo", "for", "opencode"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ignore(), "# also:begin infra") {
		t.Fatalf("empty harness block survived the last uninstall:\n%s", ignore())
	}
}

func TestFoldedModesAreFlags(t *testing.T) {
	root := NewRoot()
	audit, _, err := root.Find([]string{"audit"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"drift", "verify", "policy"} {
		if audit.Flags().Lookup(flag) == nil {
			t.Fatalf("audit is missing --%s", flag)
		}
	}
	list, _, err := root.Find([]string{"list"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"tree", "harnesses", "versions", "installs"} {
		if list.Flags().Lookup(flag) == nil {
			t.Fatalf("list is missing --%s", flag)
		}
	}
	doctor, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	if doctor.Flags().Lookup("fix") == nil {
		t.Fatal("doctor is missing --fix")
	}
}
