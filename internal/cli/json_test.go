package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runCLICapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runCLI(args...)
	_ = w.Close()
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), runErr
}

func decodeJSON(t *testing.T, out string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out)
	}
	return v
}

func TestJSONCommands(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)

	out, err := runCLICapture(t, "version", "--json")
	if err != nil {
		t.Fatal(err)
	}
	v := decodeJSON(t, out).(map[string]any)
	if v["version"] != "1.0.0" {
		t.Fatalf("version = %v", v["version"])
	}

	out, err = runCLICapture(t, "setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal", "--json")
	if err != nil {
		t.Fatal(err)
	}
	setup := decodeJSON(t, out).(map[string]any)
	if setup["default_namespace"] != "personal" || setup["harness"] != "claude" {
		t.Fatalf("unexpected setup JSON: %v", setup)
	}

	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runCLICapture(t, "push", bundle, "--name", "demo", "--json")
	if err != nil {
		t.Fatal(err)
	}
	push := decodeJSON(t, out).(map[string]any)
	if push["package"] != "demo" || push["version"] == "" || push["namespace"] != "personal" {
		t.Fatalf("unexpected push JSON: %v", push)
	}

	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
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
	out, err = runCLICapture(t, "install", "demo", "for", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	install := decodeJSON(t, out).(map[string]any)
	if install["package"] != "demo" || install["harness"] != "claude" || install["files"].(float64) == 0 {
		t.Fatalf("unexpected install JSON: %v", install)
	}

	out, err = runCLICapture(t, "doctor", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if checks, ok := decodeJSON(t, out).([]any); !ok || len(checks) == 0 {
		t.Fatalf("unexpected doctor JSON: %s", out)
	}
	out, err = runCLICapture(t, "verify", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if checks, ok := decodeJSON(t, out).([]any); !ok || len(checks) == 0 {
		t.Fatalf("unexpected verify JSON: %s", out)
	}

	out, err = runCLICapture(t, "uninstall", "demo", "--all", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if removed, ok := decodeJSON(t, out).([]any); !ok || len(removed) != 1 {
		t.Fatalf("unexpected uninstall JSON: %s", out)
	}
}

func TestUsageErrorsExitNonZero(t *testing.T) {
	setupHome(t)
	for _, args := range [][]string{
		{"install"},
		{"connect", "a", "b", "c"},
		{"verify", "a", "b"},
		{"repair", "--all", "extra"},
	} {
		if err := runCLI(args...); err == nil {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestUpgradeAndReshapeJSON(t *testing.T) {
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("push", bundle, "--name", "demo"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
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
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "CLAUDE.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("push", bundle, "--name", "demo"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLICapture(t, "upgrade", "--json")
	if err != nil {
		t.Fatal(err)
	}
	up := decodeJSON(t, out).([]any)
	if len(up) != 1 || up[0].(map[string]any)["status"] != "upgraded" {
		t.Fatalf("unexpected upgrade JSON: %s", out)
	}
	out, err = runCLICapture(t, "reshape", "opencode", "--json")
	if err != nil {
		t.Fatal(err)
	}
	rs := decodeJSON(t, out).([]any)
	if len(rs) != 1 || rs[0].(map[string]any)["to"] != "opencode" {
		t.Fatalf("unexpected reshape JSON: %s", out)
	}
}
