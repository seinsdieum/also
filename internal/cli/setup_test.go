package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

func runCLI(args ...string) error {
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

func setupRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "--bare", "-q", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return remote
}

func setupHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	t.Setenv("ALSO_HOME", filepath.Join(tmp, "also"))
	if err := os.MkdirAll(os.Getenv("HOME"), 0o755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestSetupNonInteractiveAndIdempotent(t *testing.T) {
	setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "opencode", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(os.Getenv("ALSO_HOME"), "config.json")
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Schema           string                       `json:"schema"`
		DefaultNamespace string                       `json:"default_namespace"`
		DefaultHarness   string                       `json:"default_harness"`
		Namespaces       map[string]map[string]string `json:"namespaces"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Schema != "also.config/v2" || parsed.DefaultNamespace != "personal" || parsed.DefaultHarness != "opencode" {
		t.Fatalf("unexpected config: %+v", parsed)
	}
	if parsed.Namespaces["personal"]["remote"] != remote {
		t.Fatalf("remote = %q, want %q", parsed.Namespaces["personal"]["remote"], remote)
	}
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"personal": {`) != 1 {
		t.Fatalf("namespace was duplicated:\n%s", data)
	}
	if !strings.Contains(string(data), `"claude"`) {
		t.Fatalf("second setup did not update the harness:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("ALSO_HOME"), "sets")); !os.IsNotExist(err) {
		t.Fatal("setup created ~/.also/sets")
	}
}

func TestSetupBadNamespaceDoesNotPartiallyWriteConfig(t *testing.T) {
	setupHome(t)
	remote := setupRemote(t)
	err := runCLI("setup", "--yes", "--harness", "opencode",
		"--namespace", "good="+remote,
		"--namespace", "bad="+filepath.Join(t.TempDir(), "missing.git"))
	if err == nil {
		t.Fatal("expected an error for an unreachable namespace")
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("ALSO_HOME"), "config.json")); !os.IsNotExist(statErr) {
		t.Fatal("config was written even though setup failed")
	}
}

func TestSetupYesRequiresChoices(t *testing.T) {
	setupHome(t)
	if err := runCLI("setup", "--yes"); err == nil {
		t.Fatal("expected --harness to be required")
	}
}

func TestAdoptPushesToRemoteWithoutSource(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(project, ".claude", "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude", "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := runCLI("init", "rules", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("adopt", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", remote, "tag", "--list", "also/rules/*").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("adopt did not push a version tag to the namespace")
	}
}

func TestDoctorAndAuditVerifyAfterPush(t *testing.T) {
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
	if err := runCLI("doctor"); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if err := runCLI("audit", "--verify"); err != nil {
		t.Fatalf("audit --verify: %v", err)
	}
	if err := runCLI("audit", "--verify", "--namespace", "personal", "--json"); err != nil {
		t.Fatalf("audit --verify --json: %v", err)
	}
}

func TestDoctorFixReclonesDirtyCache(t *testing.T) {
	setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	cfg, err := also.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cache := also.SetCachePath(cfg.Namespaces["personal"].Remote)
	if err := os.WriteFile(filepath.Join(cache, "junk.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("doctor", "--fix"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", cache, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("cache is still dirty:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(cache, "junk.txt")); !os.IsNotExist(err) {
		t.Fatal("repair kept an untracked file")
	}
}

func TestConnectAndDefault(t *testing.T) {
	setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("connect", "personal", remote, "--yes"); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("connect", "personal"); err != nil {
		t.Fatal(err)
	}
	cfg, err := also.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultNamespace != "personal" || cfg.Namespaces["personal"].Remote != remote {
		t.Fatalf("connect/use did not persist the namespace: %+v", cfg)
	}
	if err := runCLI("connect", "personal", filepath.Join(t.TempDir(), "other.git"), "--yes"); err == nil {
		t.Fatal("replacing a namespace without --force must fail")
	}
}

func TestPushRequiresInit(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "infra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := runCLI("push"); err == nil {
		t.Fatal("push succeeded without also init")
	}
	if err := runCLI("init", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, also.InfraFile)); err != nil {
		t.Fatalf("init did not write the marker: %v", err)
	}
	if err := runCLI("push"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", remote, "tag", "--list", "also/demo/*").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("push did not publish a version tag")
	}
}
