package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestVersionStorageUsesTreeAndTags(t *testing.T) {
	tmp, remote, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)

	v1, err := repo.Push(bundle, PushOptions{Name: "p", Label: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	// One live tree per package: manifest and origin only.
	entries, err := os.ReadDir(filepath.Join(setDir, "packages", "p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != OriginDir {
			t.Fatalf("unexpected version directory: %s", e.Name())
		}
	}
	if got := gitOut(t, setDir, "tag", "--list", versionTagName("p", v1.Version)); got == "" {
		t.Fatal("local version tag missing")
	}
	if got := gitOut(t, remote, "tag", "--list", versionTagName("p", v1.Version)); got == "" {
		t.Fatal("remote version tag missing")
	}

	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "Rules two.\n")
	v2, err := repo.Push(bundle, PushOptions{Name: "p", Label: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version == v2.Version {
		t.Fatal("changed content must produce a new version")
	}
	versions, err := repo.Versions("p")
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions: %+v %v", versions, err)
	}
	if versions[0].Version != v2.Version || versions[0].Label != "v2" {
		t.Fatalf("latest: %+v", versions[0])
	}
	old, err := repo.LoadPackage("p", v1.Version)
	if err != nil {
		t.Fatal(err)
	}
	if string(old.Files["CLAUDE.md"].Data) != "Rules one.\n" {
		t.Fatalf("old snapshot content: %q", old.Files["CLAUDE.md"].Data)
	}
	cur, err := os.ReadFile(filepath.Join(setDir, "packages", "p", OriginDir, "CLAUDE.md"))
	if err != nil || string(cur) != "Rules two.\n" {
		t.Fatalf("live tree is not the latest version: %q %v", cur, err)
	}
	if got := gitOut(t, remote, "tag", "--list", versionTagName("p", v2.Version)); got == "" {
		t.Fatal("second version tag was not pushed")
	}
}

func TestMigratePreservesVersionOrder(t *testing.T) {
	tmp, _, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)

	mk := func(label, body, date string) string {
		t.Helper()
		writeFile(t, filepath.Join(bundle, "CLAUDE.md"), body)
		snap, _, err := ReadSnapshot(bundle, SnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude", Label: label}
		meta.Digest = snap.Digest(meta)
		version := VersionFromDigest(meta.Digest)
		if err := WritePackage(filepath.Join(setDir, "packages", "p", version), meta, snap); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "-C", setDir, "add", "-A")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v %s", err, out)
		}
		cmd = exec.Command("git", "-C", setDir, "-c", "user.name=t", "-c", "user.email=t@localhost", "commit", "-qm", "legacy "+label)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v %s", err, out)
		}
		return version
	}
	v1 := mk("v1", "one\n", "2020-01-01T00:00:00Z")
	v2 := mk("v2", "two\n", "2020-01-02T00:00:00Z")

	migrated, err := repo.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 1 || migrated[0].Version != v2 {
		t.Fatalf("expected v2 to be the latest, got %+v", migrated)
	}
	versions, err := repo.Versions("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != v2 {
		t.Fatalf("version order after migration: %+v", versions)
	}
	if versions[1].Time.Year() != 2020 {
		t.Fatalf("migration lost the original version time: %+v", versions[1])
	}
	old, err := repo.LoadPackage("p", v1)
	if err != nil || string(old.Files["CLAUDE.md"].Data) != "one\n" {
		t.Fatalf("old version after migration: %v %q", err, old.Files["CLAUDE.md"].Data)
	}
}

func TestMigrateLegacyVersionDirs(t *testing.T) {
	tmp, _, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	snap, _, err := ReadSnapshot(bundle, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude", Label: "v1"}
	meta.Digest = snap.Digest(meta)
	version := VersionFromDigest(meta.Digest)
	if err := WritePackage(filepath.Join(setDir, "packages", "p", version), meta, snap); err != nil {
		t.Fatal(err)
	}
	gitOut(t, setDir, "add", "-A")
	gitOut(t, setDir, "-c", "user.name=t", "-c", "user.email=t@localhost", "commit", "-qm", "legacy")

	migrated, err := repo.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 1 || migrated[0].Package != "p" || migrated[0].Version != version {
		t.Fatalf("migration result: %+v", migrated)
	}
	if _, err := os.Stat(filepath.Join(setDir, "packages", "p", version)); !os.IsNotExist(err) {
		t.Fatal("legacy version directory was not removed")
	}
	if _, err := os.Stat(filepath.Join(setDir, "packages", "p", PackageManifestFile)); err != nil {
		t.Fatal("migrated live tree missing manifest")
	}
	if got := gitOut(t, setDir, "tag", "--list", versionTagName("p", version)); got == "" {
		t.Fatal("migration did not create a version tag")
	}
	old, err := repo.LoadPackage("p", version)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Files) != len(snap.Files) {
		t.Fatalf("migrated snapshot lost files: %d != %d", len(old.Files), len(snap.Files))
	}
	versions, err := repo.Versions("p")
	if err != nil || len(versions) != 1 || versions[0].Version != version {
		t.Fatalf("versions after migration: %+v %v", versions, err)
	}
}
