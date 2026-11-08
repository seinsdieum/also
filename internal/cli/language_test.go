package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

func TestScopedIgnoreMergesAndRemovesOnlyThatHarness(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("ALSO_HOME", filepath.Join(dir, "also"))
	if err := runIgnore([]string{"infra", "for", "opencode", "in", dir}, true); err != nil {
		t.Fatal(err)
	}
	if err := runIgnore([]string{"infra", "for", "claude", "in", dir}, true); err != nil {
		t.Fatal(err)
	}
	entries, err := also.IgnoreBlockEntries(dir, also.IgnoreInfra)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(entries, "\n")
	if !strings.Contains(joined, ".opencode/") || !strings.Contains(joined, ".claude/") {
		t.Fatalf("scoped exclude replaced instead of merging: %v", entries)
	}
	if err := runIgnore([]string{"infra", "for", "claude", "in", dir}, false); err != nil {
		t.Fatal(err)
	}
	entries, _ = also.IgnoreBlockEntries(dir, also.IgnoreInfra)
	joined = strings.Join(entries, "\n")
	if strings.Contains(joined, ".claude/") || !strings.Contains(joined, ".opencode/") {
		t.Fatalf("scoped include removed the wrong harness: %v", entries)
	}
	if err := runIgnore([]string{"infra", "in", dir}, false); err != nil {
		t.Fatal(err)
	}
	entries, _ = also.IgnoreBlockEntries(dir, also.IgnoreInfra)
	if len(entries) != 0 {
		t.Fatalf("unscoped include left entries: %v", entries)
	}
}

func TestScopedIncludeKeepsSharedInstructionEntry(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("ALSO_HOME", filepath.Join(dir, "also"))
	project, _ := also.AbsPath(dir)
	st := &also.State{Schema: also.SchemaState, Installs: []also.Install{
		{Harness: "opencode", Project: project},
		{Harness: "codex", Project: project},
	}}
	if err := also.SaveState(st); err != nil {
		t.Fatal(err)
	}
	if err := runIgnore([]string{"infra", "for", "opencode", "in", dir}, true); err != nil {
		t.Fatal(err)
	}
	if err := runIgnore([]string{"infra", "for", "codex", "in", dir}, true); err != nil {
		t.Fatal(err)
	}
	if err := runIgnore([]string{"infra", "for", "opencode", "in", dir}, false); err != nil {
		t.Fatal(err)
	}
	entries, err := also.IgnoreBlockEntries(dir, also.IgnoreInfra)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(entries, "\n")
	if !strings.Contains(joined, "AGENTS.md") {
		t.Fatalf("shared AGENTS.md entry was removed while codex remains: %v", entries)
	}
	if strings.Contains(joined, ".opencode/") || strings.Contains(joined, "opencode.json") {
		t.Fatalf("opencode-specific entries were not removed: %v", entries)
	}
}

func TestHarnessesForProjectPrefersLayoutOverDefault(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("ALSO_HOME", filepath.Join(dir, "also"))
	cfg := &also.Config{Schema: also.SchemaConfig, DefaultHarness: "claude", Namespaces: map[string]also.NamespaceConfig{}}
	if err := also.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := harnessesForProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "opencode" {
		t.Fatalf("harnesses = %v, want [opencode]; the machine default must not win over the project layout", got)
	}
}
