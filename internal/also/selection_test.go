package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialInstallSelectsOneSkill(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
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
	opts := InstallOptions{WithConfig: true, Parts: []string{"skill:review"}}
	plan, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "opencode", "project", project, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Block != nil {
		t.Fatal("instructions block must not be installed for a skill-only selection")
	}
	if len(plan.Render.MCP) != 0 || len(plan.Render.Hooks) != 0 || len(plan.Render.Settings) != 0 {
		t.Fatalf("config parts leaked into a partial install: mcp=%d hooks=%d settings=%d", len(plan.Render.MCP), len(plan.Render.Hooks), len(plan.Render.Settings))
	}
	for _, f := range plan.NewFiles {
		if !strings.Contains(f.Dest, filepath.Join(".opencode", "skills", "review")) {
			t.Fatalf("unexpected file in partial install: %s", f.Dest)
		}
	}
	if err := ApplyPlan(st, plan, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, ".opencode", "commands", "review.md"),
		filepath.Join(project, "opencode.json"),
	} {
		if _, err := os.Stat(absent); err == nil {
			t.Fatalf("partial install wrote %s", absent)
		}
	}
	st, err = LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installs := st.InstallsOf("p")
	if len(installs) != 1 || len(installs[0].Parts) != 1 || installs[0].Parts[0] != "skill:review" {
		t.Fatalf("state did not record the partial install: %+v", installs)
	}

	// A new package version must not silently install components the user
	// never selected.
	writeFile(t, filepath.Join(bundle, "skills", "extra", "SKILL.md"), "---\nname: extra\ndescription: d\n---\nextra\n")
	writeFile(t, filepath.Join(bundle, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nupdated\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	results, err := UpgradeInstalls(cfg, st, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("partial upgrade failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "extra")); err == nil {
		t.Fatal("upgrade installed an unselected component")
	}
	data, err := os.ReadFile(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "updated") {
		t.Fatalf("selected skill was not upgraded: %v %s", err, data)
	}
}

func TestPartialInstallRejectsUnknownPart(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
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
	_, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "opencode", Scope: "project", Project: project,
		Parts: []string{"skill:does-not-exist"}, WithConfig: true,
	}, "", "")
	if err == nil || !strings.Contains(err.Error(), "available parts") {
		t.Fatalf("expected an available-parts error, got: %v", err)
	}
}

func TestAdoptCapturesInstalledPartAndNewUserComponent(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
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
	opts := InstallOptions{WithConfig: true, Parts: []string{"skill:review"}}
	plan, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "opencode", "project", project, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, opts); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".opencode", "commands", "new.md"), "---\ndescription: mine\n---\nnew $ARGUMENTS\n")

	infra := filepath.Join(tmp, "infra")
	if _, err := ExtractBundle(AdoptOptions{Name: "p", Project: project, Harness: "opencode", Dest: infra}); err != nil {
		t.Fatal(err)
	}
	adopted := filepath.Join(infra, "p")
	if _, err := os.Stat(filepath.Join(adopted, "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("installed part missing from adopt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(adopted, "commands", "new.md")); err != nil {
		t.Fatalf("user-created component missing from adopt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(adopted, "commands", "review.md")); err == nil {
		t.Fatal("adopt invented a command that was never installed")
	}
}
