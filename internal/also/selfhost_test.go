package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openCodeBundle(t *testing.T, dir, agent string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(dir, "opencode.json"), `{"default_agent":"`+agent+`"}`)
	writeFile(t, filepath.Join(dir, ".opencode", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
}

func TestConfigOwnershipSameValueSurvivesUninstallOrder(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "b")
	openCodeBundle(t, a, "go-developer")
	openCodeBundle(t, b, "go-developer")
	if _, err := repo.Push(a, PushOptions{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Push(b, PushOptions{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"a", "b"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{Package: pkg, Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := LoadState()
	inA := st.InstallsOf("a")[0]
	inB := st.InstallsOf("b")[0]
	var has bool
	for _, e := range inB.Edits {
		if strings.Join(e.Path, ".") == "default_agent" {
			has = true
			if !e.BaseSet {
				t.Fatalf("second owner has no base: %+v", e)
			}
		}
	}
	if !has {
		t.Fatal("second package did not record ownership of the identical value")
	}
	if _, err := Uninstall(st, inA, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, "opencode.json"))
	if err != nil || !strings.Contains(string(data), "go-developer") {
		t.Fatalf("value of the remaining package was removed: %v %s", err, data)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("b")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("config file should be gone after the last owner: %v", err)
	}
}

func TestConfigOwnershipDifferentValueRestoresBase(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "b")
	openCodeBundle(t, a, "first")
	openCodeBundle(t, b, "second")
	if _, err := repo.Push(a, PushOptions{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Push(b, PushOptions{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "a", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "b", Harness: "opencode", Project: project, WithConfig: true, Force: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("a")[0], false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(project, "opencode.json"))
	if !strings.Contains(string(data), "second") {
		t.Fatalf("remaining package value was lost: %s", data)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("b")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("uninstall order leaked the removed package's value: %v", err)
	}
}

func TestInstallExclusionsAndAdditiveParts(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(bundle, "opencode.json"), `{"default_agent":"go-developer","model":"x/y"}`)
	writeFile(t, filepath.Join(bundle, ".opencode", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
	writeFile(t, filepath.Join(bundle, ".opencode", "agents", "dev.md"), "---\ndescription: d\nmode: primary\n---\nbody\n")
	writeFile(t, filepath.Join(bundle, ".opencode", "commands", "fix.md"), "---\ndescription: d\n---\nfix\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	// Everything except instructions and generated config.
	if _, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "opencode", Project: project, WithConfig: true,
		Exclude: []string{"instructions", "settings", "mcp"},
	}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("instructions were installed despite --except: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("skills missing: %v", err)
	}
	st, _ := LoadState()
	if got := st.InstallsOf("p")[0].Exclude; len(got) != 3 {
		t.Fatalf("exclusions were not recorded: %v", got)
	}

	// Additive: add agents to the existing skills-only install.
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true, Parts: []string{"agents"}}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("additive install removed skills: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "agents", "dev.md")); err != nil {
		t.Fatalf("agent was not added: %v", err)
	}

	// --only replaces the selection.
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true, Parts: []string{"skills"}, OnlyParts: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "agents", "dev.md")); !os.IsNotExist(err) {
		t.Fatalf("--only did not remove the old part: %v", err)
	}

	// A plain install means the whole package again, not the old selection.
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatalf("plain install did not reset the selection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "agents", "dev.md")); err != nil {
		t.Fatalf("plain install did not restore the agent: %v", err)
	}
}

func TestAuditFindsConstraintMismatch(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	openCodeBundle(t, bundle, "go-developer")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Semver: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	entry := LockEntryFromInstall(st.InstallsOf("p")[0], "^2.0.0")
	if err := SaveLock(project, &Lockfile{Packages: []LockEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "lock-constraint-mismatch" {
			return
		}
	}
	t.Fatalf("constraint mismatch was not reported: %+v", report.Findings)
}

func TestAuditFindsTrackedInfra(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	openCodeBundle(t, bundle, "go-developer")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".opencode", "AGENTS.md", "opencode.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, true)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, f := range report.Findings {
		codes[f.Code] = true
	}
	for _, code := range []string{"infra-tracked", "block-tracked", "config-tracked"} {
		if !codes[code] {
			t.Fatalf("missing finding %s: %+v", code, report.Findings)
		}
	}
}

func TestFindProjectRootFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "also.lock"), "{}")
	sub := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	if PathKey(got) != PathKey(root) {
		t.Fatalf("root = %s, want %s", got, root)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	got, err = FindProjectRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	if PathKey(got) != PathKey(root) {
		t.Fatalf("symlinked root = %s, want %s", got, root)
	}
	// A nested marker wins over the parent project.
	nested := filepath.Join(sub, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, "also.json"), "{}")
	got, err = FindProjectRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if PathKey(got) != PathKey(nested) {
		t.Fatalf("nested root = %s, want %s", got, nested)
	}
}

func TestPushRefusesPackageWithoutComponents(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, ".gitignore"), ".opencode/\nAGENTS.md\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "empty", Harness: "opencode"}); err == nil {
		t.Fatal("expected an empty package to be refused")
	}
	if _, err := repo.Push(bundle, PushOptions{Name: "empty", Harness: "opencode", AllowEmpty: true}); err != nil {
		t.Fatalf("allow-empty should publish: %v", err)
	}
}

func TestConvertAgentMapsModelsAndPermissions(t *testing.T) {
	in := []byte("---\ndescription: d\nmode: primary\nmodel: provider/model\npermission:\n  edit: deny\n  bash: deny\ntools:\n  read: true\n  edit: true\n  bash: true\n---\nbody\n")
	out, warnings, err := ConvertAgent(in, "opencode", "claude", "dev.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "provider/model") || strings.Contains(s, "permission:") {
		t.Fatalf("OpenCode-only fields leaked into Claude:\n%s", s)
	}
	if strings.Contains(s, "Edit") || strings.Contains(s, "Bash") || !strings.Contains(s, "Read") {
		t.Fatalf("deny permissions were not mapped to tools:\n%s", s)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warnings")
	}

	// A wildcard deny inside a pattern map must still remove Bash.
	in = []byte("---\ndescription: d\npermission:\n  bash:\n    \"*\": deny\n    \"git status *\": allow\n---\nbody\n")
	out, warnings, err = ConvertAgent(in, "opencode", "claude", "bm.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Bash") {
		t.Fatalf("wildcard bash deny was lost:\n%s", out)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for pattern permissions")
	}

	in = []byte("---\ndescription: d\nmodel: sonnet\npermissionMode: plan\ntools: Read, Edit, Bash\n---\nbody\n")
	out, _, err = ConvertAgent(in, "claude", "opencode", "rev.md")
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if strings.Contains(s, "sonnet") || strings.Contains(s, "permissionMode") {
		t.Fatalf("Claude-only fields leaked into OpenCode:\n%s", s)
	}
}

func TestUpgradeKeepsAbsentBaseForCreatedConfig(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "opencode.json"), `{"theme":"x","model":"m1"}`)
	writeFile(t, filepath.Join(bundle, ".opencode", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "opencode.json"), `{"theme":"x","model":"m2"}`)
	repo2 := openRepo(t, cfg)
	if _, err := repo2.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.RawAt("model"); ok {
		t.Fatalf("upgrade leaked the previous package value after uninstall: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
	if raw, ok := doc.RawAt("theme"); !ok || string(raw) != `"x"` {
		t.Fatalf("user value was lost: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
}

func TestIdenticalFilesAreAdoptedAfterStateLoss(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	openCodeBundle(t, bundle, "go-developer")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	// Simulate a restored tree: files exist, state is gone.
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatalf("identical files should be adopted without --force: %v", err)
	}
	st, _ := LoadState()
	if len(st.Installs) != 1 {
		t.Fatalf("state was not rebuilt: %+v", st.Installs)
	}
	if _, err := Uninstall(st, &st.Installs[0], false); err != nil {
		t.Fatal(err)
	}
}

func TestReshapeRemovesOldConfigEdits(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "opencode.json"), `{"default_agent":"dev","mcp":{"servers":{"x":{"type":"local","command":["/bin/echo","x"]}}}}`)
	writeFile(t, filepath.Join(bundle, ".opencode", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "opencode.json")); err != nil {
		t.Fatalf("setup did not create opencode.json: %v", err)
	}
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, []string{"p"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("reshape left the old harness config behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode")); !os.IsNotExist(err) {
		t.Fatalf("reshape left the old harness files behind: %v", err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("uninstall left the new instruction file behind: %v", err)
	}
}

func TestUpgradeStaysInMajorUnlessAsked(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "v1\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Semver: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p@1.0.0", Harness: "claude", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	in := st.InstallsOf("p")[0]
	lock := &Lockfile{}
	lock.Upsert(LockEntryFromInstall(in, ""))
	if err := SaveLock(project, lock); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "v2 breaking\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Semver: "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	results, err := UpgradeInstalls(cfg, st, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "up to date" {
		t.Fatalf("unconstrained upgrade crossed a major: %+v", results)
	}
	st, _ = LoadState()
	results, err = UpgradeInstalls(cfg, st, nil, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "upgraded" {
		t.Fatalf("--major did not upgrade: %+v", results)
	}
}

func TestReshapeIsProjectScopedByDefault(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := filepath.Join(tmp, "p1"), filepath.Join(tmp, "p2")
	for _, project := range []string{p1, p2} {
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", p1, false, nil, false); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	for _, in := range st.Installs {
		switch PathKey(in.Project) {
		case PathKey(p1):
			if in.Harness != "claude" {
				t.Fatalf("p1 was not reshaped: %+v", in)
			}
		case PathKey(p2):
			if in.Harness != "opencode" {
				t.Fatalf("reshape touched p2: %+v", in)
			}
		}
	}
}

func TestLayerConfigMergesWithMain(t *testing.T) {
	files := map[string]*SnapshotFile{
		".mcp.json":                           {Data: []byte(`{"mcpServers":{"main":{"command":"/bin/echo","args":["main"]}}}`), Mode: 0o644},
		".also-layers/opencode/opencode.json": {Data: []byte(`{"mcp":{"servers":{"layer":{"type":"local","command":["/bin/echo","layer"]}}}}`), Mode: 0o644},
	}
	pkg := &Package{Meta: PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude"}, Files: files}
	plan, err := RenderPackage(pkg, MustHarness("claude"), MustHarness("opencode").TargetPath(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.MCP["main"]; !ok {
		t.Fatalf("main MCP server was lost: %+v", plan.MCP)
	}
	if _, ok := plan.MCP["layer"]; !ok {
		t.Fatalf("layer MCP server was lost: %+v", plan.MCP)
	}
}

func TestLockNamespaceIdentity(t *testing.T) {
	lock := &Lockfile{}
	lock.Upsert(LockEntry{Namespace: "personal", Package: "p", Harness: "opencode", Version: "v1"})
	lock.Upsert(LockEntry{Namespace: "work", Package: "p", Harness: "opencode", Version: "v2"})
	if len(lock.Packages) != 2 {
		t.Fatalf("same package from two namespaces collapsed: %+v", lock.Packages)
	}
	if e := lock.FindIn("work", "p", "opencode"); e == nil || e.Version != "v2" {
		t.Fatalf("FindIn work: %+v", e)
	}
	lock.RemoveIn("personal", "p", "opencode")
	if len(lock.Packages) != 1 || lock.Packages[0].Namespace != "work" {
		t.Fatalf("RemoveIn removed the wrong entry: %+v", lock.Packages)
	}
}
