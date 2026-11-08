package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemverAliasesResolveRanges(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	first, err := repo.Push(bundle, PushOptions{Name: "p", Semver: "1.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Semver != "1.2.0" {
		t.Fatalf("expected semver 1.2.0, got %+v", first)
	}
	got, err := repo.ResolveVersion("p", "^1.0.0")
	if err != nil || got.Version != first.Version {
		t.Fatalf("^1.0.0 resolved to %+v, %v", got, err)
	}

	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "Rules two.\n")
	second, err := repo.Push(bundle, PushOptions{Name: "p", Bump: "minor"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Semver != "1.3.0" {
		t.Fatalf("expected 1.3.0, got %+v", second)
	}
	got, err = repo.ResolveVersion("p", "^1.0.0")
	if err != nil || got.Version != second.Version {
		t.Fatalf("^1.0.0 should pick 1.3.0: %+v, %v", got, err)
	}
	if _, err := repo.ResolveVersion("p", "^2.0.0"); err == nil {
		t.Fatal("^2.0.0 should not resolve")
	}
	if err := repo.SetSemver("p", "1.2.0", second.Version); err != nil {
		t.Fatalf("moving an alias must be allowed: %v", err)
	}
	moved, err := repo.ResolveVersion("p", "1.2.0")
	if err != nil || moved.Version != second.Version {
		t.Fatalf("alias did not move: %+v, %v", moved, err)
	}
}

func TestLockfileRoundTrip(t *testing.T) {
	project := t.TempDir()
	lock, err := LoadLock(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 0 {
		t.Fatal("missing lockfile must load empty")
	}
	in := &Install{
		Set: "personal", SetRemote: "git@example/repo", Package: "p",
		Version: ShortHash([]byte("p"), 16), Digest: ShortHash([]byte("d"), 64),
		Harness: "claude", SourceHarness: "claude", Parts: []string{"skills"},
		WithConfig: true, Project: project,
	}
	entry := LockEntryFromInstall(in, "^1.2.0")
	if err := entry.CheckEntry(); err != nil {
		t.Fatal(err)
	}
	lock.Upsert(entry)
	if err := SaveLock(project, lock); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLock(project)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Find("p", "claude")
	if got == nil || got.Version != in.Version || got.Constraint != "^1.2.0" {
		t.Fatalf("unexpected lock entry: %+v", got)
	}
	if err := got.VerifyInstall(in); err != nil {
		t.Fatal(err)
	}
	other := *in
	other.Version = ShortHash([]byte("other"), 16)
	if err := got.VerifyInstall(&other); err == nil {
		t.Fatal("a different version must fail the frozen check")
	}
	loaded.Remove("p", "claude")
	if got := loaded.Find("p", "claude"); got != nil {
		t.Fatal("entry was not removed")
	}
}

func TestAuditReportsDrift(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "claude", Project: project, WithConfig: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Audit(st, project, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Packages != 1 || report.Summary.Failures != 0 {
		t.Fatalf("clean install should have no failures: %+v", report.Summary)
	}
	_ = plan
	inst := st.Installs[0]
	if len(inst.Files) == 0 || inst.Block == nil {
		t.Fatal("sample install did not produce files and a block")
	}

	if err := os.WriteFile(inst.Files[0].Path, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Audit(st, project, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Modified == 0 {
		t.Fatalf("modified file was not detected: %+v", report.Findings)
	}

	if err := os.WriteFile(inst.Block.File, []byte("no managed block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Audit(st, project, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Failures == 0 {
		t.Fatalf("missing block should be a failure: %+v", report.Findings)
	}
}

func TestControlFilesAreNotPublished(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "rules\n")
	writeFile(t, filepath.Join(dir, LockFile), "{}\n")
	writeFile(t, filepath.Join(dir, PolicyFile), "schema: also.policy/v1\n")
	snap, _, err := ReadSnapshot(dir, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{LockFile, PolicyFile} {
		if _, ok := snap.Files[name]; ok {
			t.Fatalf("%s must not be published inside a package", name)
		}
	}
	if _, ok := snap.Files["CLAUDE.md"]; !ok {
		t.Fatal("harness file was dropped")
	}
}

func TestPlainTextSecretScan(t *testing.T) {
	files := map[string]*SnapshotFile{
		"notes.txt": {Data: []byte("api_key: sk-abcdefghijklmnop\n")},
		"safe.md":   {Data: []byte("api_key: ${API_KEY}\n")},
	}
	got := ScanConfigSecrets(files)
	if len(got) != 1 || !strings.Contains(got[0], "notes.txt") {
		t.Fatalf("plain-text secret was not found exactly once: %v", got)
	}
}

func TestPolicyBlocksInstall(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(project, PolicyFile)
	writeFile(t, policyPath, "schema: also.policy/v1\npackages:\n  deny: [p]\n")
	if _, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "claude", Project: project, WithConfig: true,
	}, "", ""); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("denied package was installed: %v", err)
	}

	writeFile(t, policyPath, "schema: also.policy/v1\nexecutables:\n  allow_mcp: false\n")
	if _, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "claude", Project: project, WithConfig: true,
	}, "", ""); err == nil || !strings.Contains(err.Error(), "disallowed executable") {
		t.Fatalf("MCP was installed under a policy that forbids it: %v", err)
	}

	writeFile(t, policyPath, "schema: also.policy/v1\n")
	if _, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "claude", Project: project, WithConfig: true,
	}, "", ""); err != nil {
		t.Fatalf("permissive policy should allow the install: %v", err)
	}
}
