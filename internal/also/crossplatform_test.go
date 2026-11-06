package also

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@localhost"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// A set cloned on Windows with core.autocrlf=true must keep LF on disk or the
// content digest no longer matches.
func TestEnsureLineEndingsRepairsCRLFClone(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-q")
	testGit(t, dir, "config", "core.autocrlf", "true")
	file := filepath.Join(dir, "f.txt")
	writeFile(t, file, "a\nb\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-qm", "init")

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "checkout", "--", ".")
	crlf, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(crlf), "\r\n") {
		t.Fatalf("expected the test setup to produce CRLF, got %q", crlf)
	}

	if err := ensureLineEndings(dir); err != nil {
		t.Fatal(err)
	}
	lf, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lf), "\r") {
		t.Fatalf("line endings were not repaired: %q", lf)
	}
}

// Windows has no exec bit, so LoadPackage must take modes from the git index
// for the digest to match packages pushed on Unix.
func TestLoadPackageModesFromGit(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, OriginDir)
	hook := filepath.Join(origin, "hook.sh")
	writeFile(t, hook, "#!/bin/sh\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	snap := &Snapshot{Files: map[string]*SnapshotFile{
		"hook.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755},
	}}
	meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude"}
	meta.Digest = snap.Digest(meta)
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, PackageManifestFile), string(data))

	testGit(t, dir, "init", "-q")
	testGit(t, dir, "config", "core.filemode", "true")
	testGit(t, dir, "add", ".")
	if filepath.Separator == '\\' {
		// Windows cannot set the exec bit in the worktree, but git can still
		// record it in the index; this is what a Unix push produces.
		testGit(t, dir, "update-index", "--chmod=+x", "origin/hook.sh")
	}
	testGit(t, dir, "commit", "-qm", "pkg")

	// Simulate a Windows checkout where the exec bit is gone.
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPackage(dir); err != nil {
		t.Fatalf("digest must not depend on the worktree exec bit: %v", err)
	}
}
