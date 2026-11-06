package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotExcludesAndDigest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "rules")
	writeFile(t, filepath.Join(dir, "skills", "a", "SKILL.md"), "skill")
	writeFile(t, filepath.Join(dir, ".git", "config"), "ignored")
	writeFile(t, filepath.Join(dir, "node_modules", "x", "y.js"), "ignored")
	writeFile(t, filepath.Join(dir, ".DS_Store"), "ignored")

	snap, _, err := ReadSnapshot(dir, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(snap.Files), snap.Paths())
	}
	meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude"}
	d1 := snap.Digest(meta)
	snap2, _, _ := ReadSnapshot(dir, SnapshotOptions{})
	if d2 := snap2.Digest(meta); d1 != d2 {
		t.Fatalf("digest is not deterministic: %s != %s", d1, d2)
	}
	meta.Label = "v2"
	if snap.Digest(meta) == d1 {
		t.Fatal("label must affect the version digest")
	}
}

func TestSnapshotRefusesSecrets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SECRET=1")
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "rules")
	if _, _, err := ReadSnapshot(dir, SnapshotOptions{}); err == nil {
		t.Fatal("expected secret refusal")
	}
	if _, _, err := ReadSnapshot(dir, SnapshotOptions{AllowSecrets: true}); err != nil {
		t.Fatalf("allow-secrets: %v", err)
	}
}

func TestSnapshotRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.txt"), "x")
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, err := ReadSnapshot(dir, SnapshotOptions{}); err == nil {
		t.Fatal("expected symlink refusal")
	}
}

func TestMCPSecretDetection(t *testing.T) {
	servers := map[string]MCPServer{
		"a": {Transport: "stdio", Command: []string{"x"}, Env: map[string]string{"API_KEY": "literal"}},
		"b": {Transport: "stdio", Command: []string{"x"}, Env: map[string]string{"API_KEY": "{env:API_KEY}"}},
		"c": {Transport: "stdio", Command: []string{"x"}, Env: map[string]string{"LOG_LEVEL": "info"}},
	}
	got := MCPSuspiciousSecrets(servers)
	if len(got) != 1 || got[0] != "mcp a env API_KEY" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestSnapshotRefusesOversizedPackage(t *testing.T) {
	old := maxPackageBytes
	maxPackageBytes = 10
	defer func() { maxPackageBytes = old }()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "123456")
	writeFile(t, filepath.Join(dir, "b.txt"), "123456")
	if _, _, err := ReadSnapshot(dir, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("expected total size refusal, got %v", err)
	}
	if _, _, err := ReadSnapshot(dir, SnapshotOptions{AllowLarge: true}); err != nil {
		t.Fatalf("allow-large: %v", err)
	}
}
