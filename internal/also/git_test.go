package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCloneOrInitChecksOutBranchWhenRemoteHeadIsMissing(t *testing.T) {
	tmp := t.TempDir()
	remote := filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "master", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v %s", err, out)
	}
	work := filepath.Join(tmp, "work")
	if out, err := exec.Command("git", "init", "-q", "-b", "main", work).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(work, "packages", "p", "README.md"), "hello\n")
	for _, args := range [][]string{
		{"-C", work, "add", "."},
		{"-C", work, "-c", "user.name=dev", "-c", "user.email=dev@example.invalid", "commit", "-qm", "init"},
		{"-C", work, "remote", "add", "origin", remote},
		{"-C", work, "push", "-q", "origin", "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	cache := filepath.Join(tmp, "cache")
	if _, err := cloneOrInit(remote, cache); err != nil {
		t.Fatal(err)
	}
	if !hasCommits(cache) {
		t.Fatal("clone has no checkout although the remote has a branch")
	}
	if _, err := os.Stat(filepath.Join(cache, "packages", "p", "README.md")); err != nil {
		t.Fatalf("branch worktree was not checked out: %v", err)
	}
}
