package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedUpgradeLeavesLockUntouched(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	makePkg := func(name, body string) {
		bundle := filepath.Join(tmp, name)
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "also.json"), []byte(`{"schema":"also.infra/v1","name":"`+name+`","harness":"claude"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runCLI("push", bundle); err != nil {
			t.Fatal(err)
		}
	}
	makePkg("keep", "keep v1\n")
	makePkg("deny", "deny v1\n")
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	for _, name := range []string{"keep", "deny"} {
		if err := runCLI("install", name, "for", "claude"); err != nil {
			t.Fatal(err)
		}
	}
	lockPath := filepath.Join(project, "also.lock")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	makePkg("keep", "keep v2\n")
	makePkg("deny", "deny v2\n")
	if err := os.WriteFile(filepath.Join(project, "also.policy.yml"), []byte("schema: also.policy/v1\npackages:\n  deny: [deny]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("upgrade"); err == nil {
		t.Fatal("upgrade should fail on the denied package")
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("failed upgrade changed the lock:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
