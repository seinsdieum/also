package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seinsdieum/also/internal/also"
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

func TestMultiNamespaceUpgradeKeepsOtherConstraint(t *testing.T) {
	tmp := setupHome(t)
	remote1 := setupRemote(t)
	remote2 := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "ns1="+remote1, "--namespace", "ns2="+remote2, "--default-namespace", "ns1"); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "also.json"), []byte(`{"schema":"also.infra/v1","name":"dup","harness":"claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ns := range []string{"ns1", "ns2"} {
		if err := runCLI("push", bundle, "--namespace", ns, "--semver", "0.1.0"); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := runCLI("install", "dup", "for", "claude", "from", "ns1"); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("install", "dup", "for", "claude", "from", "ns2"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("push", bundle, "--namespace", "ns1", "--semver", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("upgrade", "--major"); err != nil {
		t.Fatal(err)
	}
	lock, err := also.LoadLock(project)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ns1": "^0.2.0", "ns2": "^0.1.0"}
	for _, entry := range lock.Packages {
		if got := want[entry.Namespace]; entry.Constraint != got {
			t.Fatalf("%s constraint = %q, want %q (lock: %+v)", entry.Namespace, entry.Constraint, got, lock.Packages)
		}
	}
	if len(lock.Packages) != 2 {
		t.Fatalf("expected two lock entries, got %+v", lock.Packages)
	}
}
