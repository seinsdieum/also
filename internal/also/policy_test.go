package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePolicyFile(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, PolicyFile)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicyMissingFile(t *testing.T) {
	p, err := LoadPolicy(t.TempDir())
	if err != nil || p != nil {
		t.Fatalf("LoadPolicy = %v, %v; want nil, nil", p, err)
	}
}

func TestPolicyLoadValid(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir, `
schema: also.policy/v1
namespaces:
  allow: [personal, work]
  deny: [untrusted]
packages:
  allow: [tool-*]
  deny: [evil]
executables:
  allow_hooks: true
  allow_mcp: false
limits:
  max_package_bytes: 1234
`)
	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("LoadPolicy returned nil")
	}
	if p.Schema != SchemaPolicy {
		t.Errorf("Schema = %q", p.Schema)
	}
	if !p.NamespaceAllowed("personal") || p.NamespaceAllowed("untrusted") {
		t.Error("namespace lists not applied")
	}
	if !p.PackageAllowed("tool-a") || p.PackageAllowed("evil") {
		t.Error("package lists not applied")
	}
	if !p.ExecutablesAllowed([]string{"hook"}) {
		t.Error("allow_hooks true not applied")
	}
	if p.ExecutablesAllowed([]string{"mcp"}) {
		t.Error("allow_mcp false not applied")
	}
	if p.Limits.MaxPackageBytes != 1234 {
		t.Errorf("MaxPackageBytes = %d, want 1234", p.Limits.MaxPackageBytes)
	}
	if p.Limits.AllowLiteralSecrets {
		t.Error("AllowLiteralSecrets = true by default")
	}
}

func TestPolicyInvalidSchema(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir, "schema: also.policy/v2\n")
	if _, err := LoadPolicy(dir); err == nil {
		t.Fatal("expected error for unsupported schema")
	}
}

func TestPolicyMalformedFile(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir, "namespaces: [not, a, map\n")
	if _, err := LoadPolicy(dir); err == nil {
		t.Fatal("expected error for malformed file")
	}
}

func TestPolicyNamespacePrecedence(t *testing.T) {
	p, err := LoadPolicyFile(writePolicyFile(t, t.TempDir(), `
namespaces:
  allow: [personal, work]
  deny: [work]
`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{"personal": true, "work": false, "other": false}
	for name, want := range cases {
		if got := p.NamespaceAllowed(name); got != want {
			t.Errorf("NamespaceAllowed(%q) = %v, want %v", name, got, want)
		}
	}

	// Empty allow means any, but deny still wins.
	p, err = LoadPolicyFile(writePolicyFile(t, t.TempDir(), "namespaces:\n  deny: [bad]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.NamespaceAllowed("anything") || p.NamespaceAllowed("bad") {
		t.Error("empty namespace allow should accept all except deny")
	}
}

func TestPolicyPackageGlobs(t *testing.T) {
	p, err := LoadPolicyFile(writePolicyFile(t, t.TempDir(), `
packages:
  allow: ["tool-*", exact]
  deny: [tool-bad, "bad/*"]
`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"tool-good":  true,
		"exact":      true,
		"tool-bad":   false,
		"tool-other": true,
		"bad/one":    false,
		"other":      false,
	}
	for name, want := range cases {
		if got := p.PackageAllowed(name); got != want {
			t.Errorf("PackageAllowed(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPolicyExecutableDefaults(t *testing.T) {
	p, err := LoadPolicyFile(writePolicyFile(t, t.TempDir(), "schema: also.policy/v1\n"))
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{"hook", "mcp", "plugin", "command"}
	if !p.ExecutablesAllowed(kinds) {
		t.Error("omitted executable switches should default to true")
	}

	p, err = LoadPolicyFile(writePolicyFile(t, t.TempDir(), `
executables:
  allow_hooks: false
  allow_commands: false
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.ExecutablesAllowed([]string{"hook"}) || p.ExecutablesAllowed([]string{"command"}) {
		t.Error("explicit false not enforced")
	}
	if !p.ExecutablesAllowed([]string{"mcp", "plugin"}) {
		t.Error("omitted switches should stay true when others are false")
	}
}

func TestPolicySizeLimit(t *testing.T) {
	p, err := LoadPolicyFile(writePolicyFile(t, t.TempDir(), "limits:\n  max_package_bytes: 10\n"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*SnapshotFile{"a.txt": {Data: []byte("01234567890")}}
	got := p.CheckPackageFiles(files, nil)
	if len(got) != 1 || got[0] != "package exceeds policy limit (10 B)" {
		t.Fatalf("findings = %v", got)
	}

	// Omitted limit falls back to the package guard.
	p, err = LoadPolicyFile(writePolicyFile(t, t.TempDir(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.Limits.MaxPackageBytes != maxPackageBytes {
		t.Errorf("MaxPackageBytes = %d, want %d", p.Limits.MaxPackageBytes, maxPackageBytes)
	}
}

func TestPolicyLiteralSecrets(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadPolicyFile(writePolicyFile(t, dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*SnapshotFile{
		"config.json": {Data: []byte(`{"api_key":"sk-abcdefghijklmnop"}`)},
	}
	got := p.CheckPackageFiles(files, nil)
	if len(got) != 1 || !strings.HasPrefix(got[0], "literal secret: ") {
		t.Fatalf("findings = %v", got)
	}
	if !strings.Contains(got[0], "config.json") {
		t.Errorf("finding lacks file name: %q", got[0])
	}

	servers := map[string]MCPServer{"srv": {Env: map[string]string{"API_TOKEN": "literal-value"}}}
	if got := p.CheckPackageFiles(nil, servers); len(got) != 1 || !strings.HasPrefix(got[0], "literal secret: ") {
		t.Fatalf("mcp findings = %v", got)
	}

	// Opting in suppresses the rule.
	p, err = LoadPolicyFile(writePolicyFile(t, t.TempDir(), "limits:\n  allow_literal_secrets: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.CheckPackageFiles(files, servers); len(got) != 0 {
		t.Fatalf("findings with allow_literal_secrets = %v", got)
	}
}

func TestPolicyUnknownFields(t *testing.T) {
	p, err := LoadPolicyFile(writePolicyFile(t, t.TempDir(), `
schema: also.policy/v1
future_option: true
namespaces:
  allow: [personal]
  future_list: [x]
`))
	if err != nil {
		t.Fatalf("unknown fields rejected: %v", err)
	}
	if !p.NamespaceAllowed("personal") {
		t.Error("known fields not parsed alongside unknown ones")
	}
}
