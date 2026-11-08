package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

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
