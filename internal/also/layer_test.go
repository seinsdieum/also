package also

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayerIsAuthoritativeNativeOverride(t *testing.T) {
	mainAgent := []byte("---\ndescription: d\nmode: subagent\n---\nbody\n")
	originalAgent := []byte("---\nname: foo\ndescription: d\n---\nbody\n")
	layerAgent := originalAgent

	main := &SnapshotFile{Data: mainAgent, Mode: 0o644}
	manifest := map[string]any{
		"layers": map[string]any{
			"claude": map[string]string{
				"agents/foo.md": ShortHash(mainAgent, 64),
			},
		},
	}
	mdata, _ := json.Marshal(manifest)
	files := map[string]*SnapshotFile{
		"agents/foo.md":                     main,
		".also-layers/claude/agents/foo.md": {Data: layerAgent, Mode: 0o644},
		".also-layers/manifest.json":        {Data: mdata, Mode: 0o644},
	}
	pkg := &Package{Meta: PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "opencode"}, Files: files}
	target := MustHarness("claude").TargetPath(t.TempDir())
	plan, err := RenderPackage(pkg, MustHarness("opencode"), target)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Dest, filepath.Join("agents", "foo.md")) {
			got = f.Data
		}
	}
	if string(got) != string(originalAgent) {
		t.Fatalf("native layer should win when unchanged:\n%s", got)
	}

	// The layer is an explicit native override: it wins even if the
	// intermediate package file was edited, otherwise one package could not
	// carry two deliberately different native variants.
	modified := []byte("---\ndescription: edited\nmode: subagent\n---\nbody\n")
	files["agents/foo.md"] = &SnapshotFile{Data: modified, Mode: 0o644}
	pkg2 := &Package{Meta: pkg.Meta, Files: files}
	plan, err = RenderPackage(pkg2, MustHarness("opencode"), target)
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Dest, filepath.Join("agents", "foo.md")) {
			got = f.Data
		}
	}
	if string(got) != string(layerAgent) {
		t.Fatalf("native layer must stay authoritative:\n%s", got)
	}
}

func TestLayerManifestIsIgnoredByClassification(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "settings.json"), `{"hooks":{"Setup":[]}}`)
	pkg := &Package{Meta: PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude"}, Files: map[string]*SnapshotFile{
		"settings.json":                     {Data: []byte(`{"hooks":{"Setup":[]}}`), Mode: 0o644},
		".also-layers/claude/settings.json": {Data: []byte(`{"hooks":{"Setup":[]}}`), Mode: 0o644},
		".also-layers/manifest.json":        {Data: []byte(`{"layers":{}}`), Mode: 0o644},
	}}
	parts, err := ClassifyPackage(pkg, MustHarness("claude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append(append([]string{}, parts.Configs...), parts.Other...) {
		if strings.HasPrefix(p, ".also-layers") {
			t.Fatalf("reserved layer leaked into classification: %s", p)
		}
	}
}

func TestLayerManifestParse(t *testing.T) {
	data := []byte(`{"layers":{"claude":{"agents/x.md":"abc"}}}`)
	layers := loadLayerManifest(map[string]*SnapshotFile{
		".also-layers/manifest.json": {Data: data},
	})
	if layers["claude"]["agents/x.md"] != "abc" {
		t.Fatalf("bad manifest: %+v", layers)
	}
	_ = os.ErrNotExist
}
