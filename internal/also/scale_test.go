package also

import (
	"path/filepath"
	"testing"
	"time"
)

// TestManyVersions exercises the batch manifest path with more than one hundred
// immutable versions. It writes commits and tags directly so the test measures
// reading a large namespace, not the cost of 101 network pushes.
func TestManyVersions(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	_, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	var last string
	for i := 0; i < 101; i++ {
		snap := &Snapshot{Files: map[string]*SnapshotFile{
			"CLAUDE.md": {Data: []byte("version " + time.Unix(int64(i), 0).Format("150405") + "\n"), Mode: 0o644},
		}}
		meta := PackageMeta{
			Schema: SchemaPkg, Name: "p", SourceHarness: "claude",
			CreatedAt: time.Unix(int64(1000+i), 0).UTC(),
		}
		meta.Digest = snap.Digest(meta)
		version := VersionFromDigest(meta.Digest)
		if err := repo.removePackageTree("p"); err != nil {
			t.Fatal(err)
		}
		if err := WritePackage(filepath.Join(repo.Dir, "packages", "p"), meta, snap); err != nil {
			t.Fatal(err)
		}
		if _, err := commitPaths(repo.Dir, "version "+version, filepath.Join("packages", "p")); err != nil {
			t.Fatal(err)
		}
		if err := repo.createTag("p", version, "HEAD"); err != nil {
			t.Fatal(err)
		}
		repo.invalidateVersionData()
		last = version
	}
	versions, err := repo.Versions("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 101 {
		t.Fatalf("expected 101 versions, got %d", len(versions))
	}
	resolved, err := repo.ResolveVersion("p", last)
	if err != nil || resolved.Version != last {
		t.Fatalf("resolve latest: %+v, %v", resolved, err)
	}
	latest, err := repo.Latest("p")
	if err != nil || latest.Version != last {
		t.Fatalf("latest: %+v, %v", latest, err)
	}
}
