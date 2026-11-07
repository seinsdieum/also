package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossHarnessInstructionBlocks(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "review", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"opencode", "codex"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{
			Package: "review", Harness: harness, Scope: "project", Project: project, WithConfig: true,
		}, "", ""); err != nil {
			t.Fatalf("install for %s: %v", harness, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(project, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"also:begin personal/review/opencode", "also:begin personal/review/codex"} {
		if !strings.Contains(string(data), id) {
			t.Fatalf("missing managed block %q in:\n%s", id, data)
		}
	}
}

func TestUninstallKeepsSharedFile(t *testing.T) {
	t.Setenv("ALSO_HOME", filepath.Join(t.TempDir(), "also"))
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared.txt")
	writeFile(t, shared, "same\n")
	sha := ShortHash([]byte("same\n"), 64)
	st := &State{Schema: SchemaState, Installs: []Install{
		{ID: "a", Package: "p", Harness: "claude", Scope: "project", Project: dir,
			Files: []InstalledFile{{Path: shared, SHA: sha}}},
		{ID: "b", Package: "q", Harness: "codex", Scope: "project", Project: dir,
			Files: []InstalledFile{{Path: shared, SHA: sha}}},
	}}
	warnings, err := Uninstall(st, &st.Installs[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("shared file was removed")
	}
	if !strings.Contains(strings.Join(warnings, " "), "shared") {
		t.Fatalf("expected a shared-file warning, got %v", warnings)
	}
	if len(st.Installs) != 1 || st.Installs[0].ID != "b" {
		t.Fatalf("unexpected state: %+v", st.Installs)
	}
}

func TestLockReleaseDoesNotRemoveForeignLock(t *testing.T) {
	t.Setenv("ALSO_HOME", filepath.Join(t.TempDir(), "also"))
	lock, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(), []byte("someone-else\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock.Release()
	if _, err := os.Stat(LockPath()); err != nil {
		t.Fatal("a foreign lock file was removed")
	}
	_ = os.Remove(LockPath())
}

func TestBackupFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file")
	writeFile(t, target, "data")
	blocker := filepath.Join(dir, "blocker")
	writeFile(t, blocker, "not a directory")
	txx := &tx{snapshots: map[string]*fileSnapshot{}, backupDir: blocker}
	if err := txx.backup(target); err == nil {
		t.Fatal("expected an error when the backup directory is unusable")
	}
}

func TestReadSnapshotRespectsGitIgnore(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(dir, "keep.md"), "keep\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	writeFile(t, filepath.Join(dir, "ignored.txt"), "secret\n")
	snap, _, err := ReadSnapshot(dir, SnapshotOptions{GitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.Files["ignored.txt"]; ok {
		t.Fatal("ignored file was included")
	}
	if _, ok := snap.Files["keep.md"]; !ok {
		t.Fatal("git-visible file was dropped")
	}
}

func TestPushRefusesForeignFiles(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	writeFile(t, filepath.Join(bundle, "notes.txt"), "notes\n")
	opts := PushOptions{Name: "p", Harness: "claude", DryRun: true}
	if _, err := repo.Push(bundle, opts); err == nil || !strings.Contains(err.Error(), "--all-files") {
		t.Fatalf("expected the --all-files gate, got %v", err)
	}
	opts.AllFiles = true
	if _, err := repo.Push(bundle, opts); err != nil {
		t.Fatalf("--all-files should allow the extra file: %v", err)
	}
}
