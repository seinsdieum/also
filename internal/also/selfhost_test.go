package also

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestAuditFindsStaleHarnessFiles(t *testing.T) {
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
	writeFile(t, filepath.Join(project, ".opencode", "skills", "local", "SKILL.md"), "local\n")
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".opencode/skills/local/SKILL.md"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" {
			return
		}
	}
	t.Fatalf("stale harness file was not reported: %+v", report.Findings)
}

func TestAuditFindsStaleFilesWithEmptyLock(t *testing.T) {
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
	writeFile(t, filepath.Join(project, ".opencode", "skills", "local", "SKILL.md"), "local\n")
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".opencode/skills/local/SKILL.md"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	if err := SaveLock(project, &Lockfile{}); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" {
			return
		}
	}
	t.Fatalf("stale file with an empty lock was not reported: %+v", report.Findings)
}

func TestAuditFindsStaleOtherHarnessConfig(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	writeFile(t, filepath.Join(bundle, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo"}}}`)
	if _, err := repo.Push(bundle, PushOptions{Name: "c", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "claude", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","args":["user"]}}}`)
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "opencode", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".mcp.json")); err != nil {
		t.Fatalf("setup should leave the modified old config: %v", err)
	}
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".mcp.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ = LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" && strings.HasSuffix(f.Path, ".mcp.json") {
			return
		}
	}
	t.Fatalf("stale other-harness config was not reported: %+v", report.Findings)
}

func TestAuditDoesNotFlagUserOwnedConfig(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, ".opencode", "skills", "s", "SKILL.md"), "---\nname: s\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "opencode.json"), `{"mine":true,"mcp":{"servers":{}}}`)
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", "opencode.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" && strings.HasSuffix(f.Path, "opencode.json") {
			t.Fatalf("user-owned config was flagged: %+v", report.Findings)
		}
	}
}

func TestAuditStaleRootConfigIgnoresNestedHarness(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	oc := filepath.Join(tmp, "oc")
	writeFile(t, filepath.Join(oc, ".opencode", "skills", "s", "SKILL.md"), "---\nname: s\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(oc, PushOptions{Name: "o", Harness: "opencode"}); err != nil {
		t.Fatal(err)
	}
	cl := filepath.Join(tmp, "cl")
	writeFile(t, filepath.Join(cl, "CLAUDE.md"), "rules\n")
	if _, err := repo.Push(cl, PushOptions{Name: "c", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "mono")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(root, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "o", Harness: "opencode", Project: root}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "claude", Project: sub}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo"}}}`)
	if out, err := runGit(root, "add", "-f", ".mcp.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, root, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" && strings.HasSuffix(f.Path, ".mcp.json") {
			return
		}
	}
	t.Fatalf("nested same-harness install hid a root stale config: %+v", report.Findings)
}

func TestAuditFindsStaleConfigAfterUninstall(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	claude := filepath.Join(tmp, "claude")
	writeFile(t, filepath.Join(claude, "CLAUDE.md"), "rules\n")
	writeFile(t, filepath.Join(claude, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo"}}}`)
	if _, err := repo.Push(claude, PushOptions{Name: "c", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	oc := filepath.Join(tmp, "oc")
	writeFile(t, filepath.Join(oc, ".opencode", "skills", "s", "SKILL.md"), "---\nname: s\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(oc, PushOptions{Name: "o", Harness: "opencode"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "claude", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "o", Harness: "opencode", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","args":["user"]}}}`)
	st, _ := LoadState()
	var claudeIn *Install
	for _, in := range st.InstallsOf("c") {
		claudeIn = in
	}
	if _, err := Uninstall(st, claudeIn, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".mcp.json")); err != nil {
		t.Fatalf("setup should leave the modified config: %v", err)
	}
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".mcp.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ = LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" && strings.HasSuffix(f.Path, ".mcp.json") {
			return
		}
	}
	t.Fatalf("stale config after uninstall was not reported: %+v", report.Findings)
}

func TestAuditFindsTrackedExecutableConfig(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "c", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "claude", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	// A tracked OpenCode config is executable surface even though OpenCode is
	// not the installed harness. The .jsonc spelling must be covered too.
	for _, name := range []string{".mcp.json", "opencode.jsonc"} {
		writeFile(t, filepath.Join(project, name), `{"mcp":{"servers":{"x":{"type":"local","command":["/bin/echo","x"]}}}}`)
	}
	nested := filepath.Join(project, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo"}}}`)
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".mcp.json", "opencode.jsonc", "sub/.mcp.json"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" {
			paths[filepath.ToSlash(f.Path)] = true
		}
	}
	for _, suffix := range []string{"/.mcp.json", "/opencode.jsonc", "/sub/.mcp.json"} {
		found := false
		for path := range paths {
			if strings.HasSuffix(path, suffix) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tracked executable config %s was not reported: %+v", suffix, report.Findings)
		}
	}
}

func TestAuditFindsStaleOtherHarnessDirectory(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "c"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "claude", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".opencode", "notes.txt"), "stale\n")
	if out, err := runGit(project, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := runGit(project, "add", "-f", ".opencode/notes.txt"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" {
			return
		}
	}
	t.Fatalf("stale other-harness directory was not reported: %+v", report.Findings)
}

func TestAuditFindsStaleNestedProject(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	openCodeBundle(t, bundle, "go-developer")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "mono")
	project := filepath.Join(root, "sub")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(root, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".opencode", "skills", "local", "SKILL.md"), "local\n")
	if out, err := runGit(root, "add", "-f", "sub/.opencode/skills/local/SKILL.md"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "stale-infra" {
			return
		}
	}
	t.Fatalf("nested stale harness file was not reported: %+v", report.Findings)
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

func TestCodexCreatedConfigRemovedAfterStateLoss(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(bundle, "config.toml"), "[mcp_servers.x]\ncommand = \"/bin/echo\"\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "c", Harness: "codex"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "codex", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "config.toml")); err != nil {
		t.Fatalf("codex config was not created: %v", err)
	}
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "c", Harness: "codex", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("c")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("state-loss uninstall left the created codex config: %v", err)
	}
}

func TestTOMLManagedBlockIsAdoptedAfterStateLoss(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	block := "[mcp_servers.x]\ncommand = \"/bin/echo\"\n"
	writeFile(t, file, string(tomlAppendBlock(nil, "mcp:x", block)))
	edit, _, err := planTOMLMCP(&State{}, nil, file, "x", block, false)
	if err != nil {
		t.Fatalf("existing managed block was not adopted: %v", err)
	}
	tx := newTx()
	if err := applyConfigEdit(tx, *edit); err != nil {
		t.Fatal(err)
	}
	data := mustReadTestFile(t, file)
	if strings.Count(string(data), "# also:begin mcp:x") != 1 {
		t.Fatalf("TOML block was duplicated: %s", data)
	}
	// A user edit inside the managed block needs --force.
	edited := strings.Replace(string(data), `/bin/echo`, `/bin/evil`, 1)
	writeFile(t, file, edited)
	if _, _, err := planTOMLMCP(&State{}, nil, file, "x", block, false); err == nil {
		t.Fatal("a modified TOML block must require --force")
	}
	if _, _, err := planTOMLMCP(&State{}, nil, file, "x", block, true); err != nil {
		t.Fatalf("--force should allow the TOML replacement: %v", err)
	}
}

func TestPushRefusesManagedInstructionBlock(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "<!-- also:begin p/opencode -->\nrules\n<!-- also:end p/opencode -->\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "opencode"}); err == nil {
		t.Fatal("expected a managed block to be refused")
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
	// A truly empty source (no files at all) must honor the same flag.
	trulyEmpty := filepath.Join(tmp, "truly-empty")
	if err := os.MkdirAll(trulyEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Push(trulyEmpty, PushOptions{Name: "empty2", Harness: "opencode"}); err == nil {
		t.Fatal("an empty source must be refused by default")
	}
	if _, err := repo.Push(trulyEmpty, PushOptions{Name: "empty2", Harness: "opencode", AllowEmpty: true}); err != nil {
		t.Fatalf("allow-empty should publish an empty source: %v", err)
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

func TestStateLossRemovesCreatedConfigFile(t *testing.T) {
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
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("created config file survived state-loss uninstall: %v", err)
	}
}

func TestUserEditAfterStateLossNeedsForce(t *testing.T) {
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
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x","default_agent":"mine"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err == nil {
		t.Fatal("a user edit after state loss must not be overwritten without --force")
	}
	if !strings.Contains(string(mustReadTestFile(t, filepath.Join(project, "opencode.json"))), "mine") {
		t.Fatal("the user edit was lost")
	}
}

func TestReinstallAfterManualDeletionKeepsAbsence(t *testing.T) {
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
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	var target *Install
	for _, in := range st.InstallsOf("p") {
		if in.Harness == "opencode" {
			target = in
		}
	}
	if target == nil {
		t.Fatal("opencode install missing")
	}
	if _, err := Uninstall(st, target, false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.RawAt("default_agent"); ok {
		t.Fatalf("uninstall resurrected a deleted key: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
}

func TestForcedReinstallRefreshesBaseAfterManualRecreation(t *testing.T) {
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
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x","default_agent":"user-original"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true, Force: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	var target *Install
	for _, in := range st.InstallsOf("p") {
		if in.Harness == "opencode" {
			target = in
			break
		}
	}
	if target == nil {
		t.Fatal("opencode install missing")
	}
	if _, err := Uninstall(st, target, false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := doc.RawAt("default_agent"); !ok || string(raw) != `"user-original"` {
		t.Fatalf("forced reinstall reused a stale base: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
}

func TestReshapeCleansOldFileProvenance(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	for _, name := range []string{"first", "second"} {
		bundle := filepath.Join(tmp, name)
		openCodeBundle(t, bundle, "dev-"+name)
		if _, err := repo.Push(bundle, PushOptions{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "first", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	// The user recreates the old harness files by hand.
	writeFile(t, filepath.Join(project, "AGENTS.md"), "\n")
	writeFile(t, filepath.Join(project, "opencode.json"), "{}\n")
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "second", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("second")[0], false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "opencode.json"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Fatalf("a stale origin deleted user file %s: %v", name, err)
		}
	}
}

func TestReshapeStateLossRemovesNewHarnessFiles(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "rules\n")
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
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "claude", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Fatalf("state-loss uninstall left %s: %v", name, err)
		}
	}
}

func TestConfigOriginsResetAfterLastUninstall(t *testing.T) {
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
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	// The user now owns the same value the package used to manage.
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x","default_agent":"go-developer"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := doc.RawAt("default_agent"); !ok || string(raw) != `"go-developer"` {
		t.Fatalf("a stale origin deleted the user's value: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
}

func TestStateLossKeepsConfigOrigins(t *testing.T) {
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
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	// Simulate state loss; config origins live outside state.
	if err := SaveState(&State{Schema: SchemaState}); err != nil {
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
	if _, ok := doc.RawAt("default_agent"); ok {
		t.Fatalf("state-loss uninstall left default_agent: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
	if raw, ok := doc.RawAt("theme"); !ok || string(raw) != `"x"` {
		t.Fatalf("user value was lost: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("state-loss uninstall left the created instruction file: %v", err)
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

func TestReshapePreflightLeavesNothingHalfMigrated(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	for _, name := range []string{"a1", "a2"} {
		bundle := filepath.Join(tmp, name)
		writeFile(t, filepath.Join(bundle, ".opencode", "agents", name+".md"), "---\ndescription: d\nmode: primary\n---\nbody\n")
		if _, err := repo.Push(bundle, PushOptions{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a1", "a2"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{Package: name, Harness: "opencode", Project: project}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	// A pre-existing target file for the second package makes the preflight
	// fail; the first package must not be migrated.
	writeFile(t, filepath.Join(project, ".claude", "agents", "a2.md"), "conflict\n")
	st, _ := LoadState()
	results, err := Reshape(cfg, st, "claude", project, false, nil, false)
	if err != nil {
		t.Fatalf("preflight should report per-package errors: %v", err)
	}
	failed := false
	for _, r := range results {
		if r.Err != nil {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("expected a preflight failure: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "agents", "a1.md")); !os.IsNotExist(err) {
		t.Fatalf("reshape applied a1 before failing a2: %v", err)
	}
	st, _ = LoadState()
	for _, in := range st.Installs {
		if in.Project != "" && in.Harness == "claude" {
			t.Fatalf("state was mutated by a failed preflight: %+v", in)
		}
	}
}

func TestReshapeRoundTripKeepsUserConfig(t *testing.T) {
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
	writeFile(t, filepath.Join(project, "opencode.json"), `{"theme":"x","default_agent":"user-agent"}`)
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project, WithConfig: true, Force: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if _, err := Reshape(cfg, st, "opencode", project, false, nil, false); err != nil {
		t.Fatalf("opencode -> claude -> opencode failed: %v", err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := doc.RawAt("default_agent"); !ok || string(raw) != `"user-agent"` {
		t.Fatalf("round-trip lost the user value: %s", mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	}
}

func TestReshapeKeepsSemver(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, ".opencode", "skills", "s", "SKILL.md"), "---\nname: s\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Semver: "1.2.3"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project}, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState()
	if _, err := Reshape(cfg, st, "claude", project, false, nil, false); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if got := st.InstallsOf("p")[0].Semver; got != "1.2.3" {
		t.Fatalf("reshape lost semver: %q", got)
	}
}

func TestRootPolicyProtectsNestedProject(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "mono")
	project := filepath.Join(root, "product")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(root, "init", "-q"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(root, "also.policy.yml"), "schema: also.policy/v1\npackages:\n  deny: [p]\n")
	// A permissive nested policy must not disable the root deny.
	writeFile(t, filepath.Join(project, "also.policy.yml"), "schema: also.policy/v1\n")
	if _, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "claude", Project: project}, "", ""); err == nil {
		t.Fatal("nested project ignored the root policy")
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("denied install wrote files: %v", err)
	}
}

func TestReshapeEnforcesPolicy(t *testing.T) {
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
	writeFile(t, filepath.Join(project, "also.policy.yml"), "schema: also.policy/v1\npackages:\n  deny: [p]\n")
	st, _ := LoadState()
	results, err := Reshape(cfg, st, "claude", project, false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for _, r := range results {
		if r.Err != nil {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("reshape ignored a denying policy: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("failed reshape removed the opencode install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("failed reshape wrote the target harness: %v", err)
	}
}

func TestReshapeRollsBackAppliedPackagesOnWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based write failure is not reliable on Windows")
	}
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	p1 := filepath.Join(tmp, "p1")
	writeFile(t, filepath.Join(p1, ".opencode", "agents", "a1.md"), "---\ndescription: d\nmode: primary\n---\nbody\n")
	if _, err := repo.Push(p1, PushOptions{Name: "p1"}); err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(tmp, "p2")
	writeFile(t, filepath.Join(p2, ".opencode", "commands", "c2.md"), "---\ndescription: d\n---\nbody\n")
	if _, err := repo.Push(p2, PushOptions{Name: "p2"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"p1", "p2"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{Package: name, Harness: "opencode", Project: project}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	commands := filepath.Join(project, ".claude", "commands")
	if err := os.MkdirAll(commands, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(commands, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(commands, 0o755) })
	st, _ := LoadState()
	_, err := Reshape(cfg, st, "claude", project, false, nil, false)
	if err == nil {
		t.Fatal("expected an apply-time reshape failure")
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "agents", "a1.md")); err != nil {
		t.Fatalf("rollback did not restore the opencode agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "agents", "a1.md")); !os.IsNotExist(err) {
		t.Fatalf("rollback left the claude agent: %v", err)
	}
	st, _ = LoadState()
	for _, in := range st.Installs {
		if in.Project != "" && in.Harness == "claude" {
			t.Fatalf("state was not rolled back: %+v", in)
		}
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
	origins, _ := loadConfigOrigins(project)
	if len(origins.Entries) != 0 || len(origins.Blocks) != 0 || len(origins.Files) != 0 {
		t.Fatalf("origins survived the last uninstall: %+v", origins)
	}
}

func TestUpgradePolicyIsAtomic(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	makePkg := func(name, body string) {
		bundle := filepath.Join(tmp, name)
		writeFile(t, filepath.Join(bundle, "CLAUDE.md"), body)
		if _, err := repo.Push(bundle, PushOptions{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	makePkg("keep", "keep v1\n")
	makePkg("deny", "deny v1\n")
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keep", "deny"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{Package: name, Harness: "claude", Project: project}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	makePkg("keep", "keep v2\n")
	makePkg("deny", "deny v2\n")
	writeFile(t, filepath.Join(project, "also.policy.yml"), "schema: also.policy/v1\npackages:\n  deny: [deny]\n")
	st, _ := LoadState()
	before := map[string]string{}
	for _, in := range st.InstallsOf("") {
		before[in.Package] = in.Version
	}
	results, err := UpgradeInstalls(cfg, st, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for _, r := range results {
		if r.Err != nil {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("policy denial was not reported: %+v", results)
	}
	st, _ = LoadState()
	for _, name := range []string{"keep", "deny"} {
		if got := st.InstallsOf(name)[0].Version; got != before[name] {
			t.Fatalf("%s was upgraded despite a failed preflight: %s -> %s", name, before[name], got)
		}
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
