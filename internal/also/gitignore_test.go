package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

func TestGitIgnoreInfraAndAlsoBlocks(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	ignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignore, []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := InfraIgnoreEntries(nil)
	for _, want := range []string{"CLAUDE.md", ".claude/", ".opencode/", ".codex/", ".agents/", ".qwen/"} {
		if !contains(entries, want) {
			t.Fatalf("infra entries missing %q: %v", want, entries)
		}
	}
	changed, err := UpdateGitIgnore(dir, IgnoreInfra, true, entries)
	if err != nil || !changed {
		t.Fatalf("add infra: changed=%v err=%v", changed, err)
	}
	changed, err = UpdateGitIgnore(dir, IgnoreInfra, true, entries)
	if err != nil || changed {
		t.Fatalf("second add should be idempotent: changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(ignore)
	text := string(data)
	if !strings.Contains(text, "node_modules/") || !strings.Contains(text, ignoreBegin(IgnoreInfra)) || !strings.Contains(text, ".claude/") {
		t.Fatalf("bad infra block:\n%s", text)
	}

	if _, err := UpdateGitIgnore(dir, IgnoreAlso, true, AlsoIgnoreEntries()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(ignore)
	if !strings.Contains(string(data), ".also-layers/") {
		t.Fatalf("also block missing:\n%s", data)
	}

	if changed, err := UpdateGitIgnore(dir, IgnoreInfra, false, nil); err != nil || !changed {
		t.Fatalf("remove infra: changed=%v err=%v", changed, err)
	}
	data, _ = os.ReadFile(ignore)
	text = string(data)
	if strings.Contains(text, ignoreBegin(IgnoreInfra)) || strings.Contains(text, ".claude/") {
		t.Fatalf("infra block was not removed:\n%s", text)
	}
	if !strings.Contains(text, "node_modules/") || !strings.Contains(text, ".also-layers/") {
		t.Fatalf("unrelated lines were damaged:\n%s", text)
	}

	if changed, err := UpdateGitIgnore(dir, IgnoreAlso, false, nil); err != nil || !changed {
		t.Fatalf("remove also: changed=%v err=%v", changed, err)
	}
	data, _ = os.ReadFile(ignore)
	if strings.Contains(string(data), "also:begin") {
		t.Fatalf("also block was not removed:\n%s", data)
	}
	if !strings.Contains(string(data), "node_modules/") {
		t.Fatalf("user line was lost:\n%s", data)
	}
}

func TestGitProjectRootFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := GitProjectRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ = filepath.EvalSymlinks(got)
	if got != want {
		t.Fatalf("root = %s, want %s", got, want)
	}
}

func TestGitIgnoreInfraForSelectedHarness(t *testing.T) {
	entries := InfraIgnoreEntries([]string{"claude"})
	if !contains(entries, ".claude/") || !contains(entries, "CLAUDE.md") {
		t.Fatalf("claude entries missing: %v", entries)
	}
	if contains(entries, ".opencode/") {
		t.Fatalf("unselected harness leaked into entries: %v", entries)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestGitIgnoreKeepsModeAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	ignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(ignore, []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateGitIgnore(dir, IgnoreInfra, true, []string{".claude/"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ignore)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed: %o", info.Mode().Perm())
	}

	linkDir := t.TempDir()
	gitInit(t, linkDir)
	target := filepath.Join(linkDir, "real-ignore")
	if err := os.WriteFile(target, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real-ignore", filepath.Join(linkDir, ".gitignore")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := UpdateGitIgnore(linkDir, IgnoreInfra, true, nil); err == nil {
		t.Fatal("expected symlink refusal")
	}
}
