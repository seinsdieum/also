package also

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func adapterJSON(id string) string {
	return `{
  "id": "` + id + `",
  "display": "Acme Unit",
  "global_base": "~/.` + id + `",
  "global": {"instr": "AGENTS.md", "skills": "skills", "mcp": "mcp.json"},
  "project": {"instr": "AGENTS.md", "skills": ".` + id + `/skills", "mcp": ".` + id + `/mcp.json"},
  "bundle": {"instr": "AGENTS.md", "skills": "skills", "mcp": ".mcp.json"},
  "roots": ["", ".` + id + `"],
  "mcp": {"format": "claude-json", "configs": [".mcp.json"]},
  "components": [{"kind": "workflows", "bundle_dir": "workflows", "project_dir": ".` + id + `/workflows"}],
  "detect": [{"paths": [".` + id + `"], "weight": 5}]
}`
}

func TestAddAdapterFromFileAndURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ALSO_ADAPTERS", dir)
	resetExternalAdapters()
	defer resetExternalAdapters()

	src := filepath.Join(t.TempDir(), "acme.json")
	if err := os.WriteFile(src, []byte(adapterJSON("acmeunit")), 0o644); err != nil {
		t.Fatal(err)
	}
	h, dest, err := AddAdapterFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "acmeunit" || dest != filepath.Join(dir, "acmeunit.json") {
		t.Fatalf("unexpected add result: %+v %s", h, dest)
	}
	if got, ok := ResolveHarness("acmeunit"); !ok || got.ID != "acmeunit" {
		t.Fatalf("new adapter is not resolvable: %v %v", got, ok)
	}
	if _, _, err := AddAdapterFile(src); err != nil {
		t.Fatalf("re-adding the same adapter should update it: %v", err)
	}

	builtin := filepath.Join(t.TempDir(), "claude.json")
	if err := os.WriteFile(builtin, []byte(adapterJSON("claude")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddAdapterFile(builtin); err == nil {
		t.Fatal("a community adapter must not shadow a built-in id")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(adapterJSON("acmeurl")))
	}))
	defer server.Close()
	h2, _, err := AddAdapterFile(server.URL + "/acme.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ResolveHarness("acmeurl"); !ok {
		t.Fatal("adapter from URL was not registered")
	}

	removed, err := RemoveAdapter(h2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("adapter file still exists: %v", err)
	}
	if _, ok := ResolveHarness("acmeurl"); ok {
		t.Fatal("removed adapter is still resolvable")
	}
	if _, err := RemoveAdapter("claude"); err == nil {
		t.Fatal("built-in adapters must not be removable")
	}
}

func TestExternalAdapterInstallsWithoutRecompiling(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ALSO_ADAPTERS", dir)
	resetExternalAdapters()
	defer resetExternalAdapters()

	src := filepath.Join(t.TempDir(), "acme.json")
	if err := os.WriteFile(src, []byte(adapterJSON("acmeinstall")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddAdapterFile(src); err != nil {
		t.Fatal(err)
	}

	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "Acme rules.\n")
	writeFile(t, filepath.Join(bundle, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
	writeFile(t, filepath.Join(bundle, "workflows", "deploy.md"), "deploy\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "acmeinstall"}); err != nil {
		t.Fatal(err)
	}
	pkg, err := repo.LoadPackage("p", mustLatest(t, repo, "p").Version)
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	st := &State{Schema: SchemaState}
	plan, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "acmeinstall", "project", project, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".acmeinstall", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("community adapter did not install its skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatalf("community adapter did not install instructions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".acmeinstall", "workflows", "deploy.md")); err != nil {
		t.Fatalf("community adapter did not install its custom component: %v", err)
	}
}
