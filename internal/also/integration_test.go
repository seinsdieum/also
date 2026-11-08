package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (tmp, remote, setDir string, cfg *Config) {
	t.Helper()
	tmp = t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	setTestHome(t, home)
	t.Setenv("ALSO_HOME", filepath.Join(tmp, "also"))
	remote = filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	setDir = SetCachePath(remote)
	cfg = &Config{
		Schema:     SchemaConfig,
		Namespaces: map[string]NamespaceConfig{"personal": {Remote: remote}},
	}
	return tmp, remote, setDir, cfg
}

func openRepo(t *testing.T, cfg *Config) *SetRepo {
	t.Helper()
	r, err := OpenSet("personal", cfg.Namespaces["personal"], false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sampleBundle(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "Rules one.\n")
	writeFile(t, filepath.Join(dir, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "review", "scripts", "run.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, "commands", "review.md"), "---\ndescription: d\n---\nReview $ARGUMENTS\n")
	writeFile(t, filepath.Join(dir, "agents", "critic.md"), "---\nname: critic\ndescription: d\ntools: Read, Grep\n---\nbody\n")
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","args":["a"]}}}`)
	writeFile(t, filepath.Join(dir, "settings.json"), `{"permissions":{"allow":["Bash(ls:*)"]},"statusLine":{"type":"command","command":"echo status"},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hook"}]}]}}`)
	writeFile(t, filepath.Join(dir, "hooks", "guard.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, "status_lines", "line.py"), "# status line\n")
}

func TestEndToEnd(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)

	res, err := repo.Push(bundle, PushOptions{Name: "p", Label: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Existing || res.Version == "" {
		t.Fatalf("bad push result: %+v", res)
	}
	res2, err := repo.Push(bundle, PushOptions{Name: "p", Label: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Existing {
		t.Fatal("second push with identical content must be a no-op")
	}
	versions, err := repo.Versions("p")
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions: %v %v", versions, err)
	}
	vs, err := repo.ResolveVersion("p", "v1")
	if err != nil || vs.Version != res.Version {
		t.Fatalf("label resolution: %+v %v", vs, err)
	}
	pkg, err := repo.LoadPackage("p", res.Version)
	if err != nil {
		t.Fatal(err)
	}

	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	st := &State{Schema: SchemaState}
	plan, err := BuildPlan(st, repo, pkg, vs, "opencode", "project", project, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(project, "AGENTS.md")
	if data, err := os.ReadFile(agents); err != nil || !strings.Contains(string(data), "Rules one.") {
		t.Fatalf("instructions: %v %s", err, data)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "commands", "review.md")); err != nil {
		t.Fatal(err)
	}
	ocCfg := filepath.Join(project, "opencode.json")
	data, err := os.ReadFile(ocCfg)
	if err != nil || !strings.Contains(string(data), `"x"`) {
		t.Fatalf("opencode mcp: %v %s", err, data)
	}
	// Idempotent reinstall.
	st, _ = LoadState()
	plan, err = BuildPlan(st, repo, pkg, vs, "opencode", "project", project, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}

	// Local modification blocks upgrade without --force.
	skillFile := filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")
	writeFile(t, skillFile, "---\nname: review\ndescription: d\n---\nLOCAL EDIT\n")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "Rules two.\n")
	res3, err := repo.Push(bundle, PushOptions{Name: "p", Label: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	results, err := UpgradeInstalls(cfg, st, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("upgrade should refuse a modified file: %+v", results)
	}
	st, _ = LoadState()
	results, err = UpgradeInstalls(cfg, st, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("forced upgrade failed: %+v", results)
	}
	data, _ = os.ReadFile(agents)
	if !strings.Contains(string(data), "Rules two.") {
		t.Fatalf("instructions not upgraded: %s", data)
	}
	_ = res3

	// Reshape to codex.
	st, _ = LoadState()
	rr, err := Reshape(cfg, st, "codex", project, false, []string{"p"}, false)
	if err != nil || len(rr) != 1 || rr[0].Err != nil {
		t.Fatalf("reshape: %+v %v", rr, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("codex skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err == nil {
		t.Fatal("opencode files must be removed after reshape")
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "hooks.json")); err != nil {
		t.Fatalf("codex hooks missing: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(project, ".codex", "config.toml"))
	if !strings.Contains(string(data), "[mcp_servers.x]") {
		t.Fatalf("codex mcp missing: %s", data)
	}

	// Uninstall removes only the created instructions file.
	st, _ = LoadState()
	in := st.InstallsOf("p")[0]
	if _, err := Uninstall(st, in, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agents); err == nil {
		t.Fatal("instructions file created by also should be removed")
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "review")); err == nil {
		t.Fatal("skill should be removed")
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "hooks.json")); err == nil {
		t.Fatal("config file created by also should be removed when it becomes empty")
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "config.toml")); err == nil {
		t.Fatal("config file created by also should be removed when it becomes empty")
	}
}

func TestUserFilePreservedAndMCPRestore(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "PACKAGE RULES\n")
	writeFile(t, filepath.Join(bundle, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","args":["new"]}}}`)
	writeFile(t, filepath.Join(bundle, "settings.json"), `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo new"}]}]}}`)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	pkg, _ := repo.LoadPackage("p", mustLatest(t, repo, "p").Version)
	project := filepath.Join(tmp, "project")
	writeFile(t, filepath.Join(project, "CLAUDE.md"), "USER RULES\n")
	writeFile(t, filepath.Join(project, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/true"}}}`)
	writeFile(t, filepath.Join(project, ".claude", "settings.json"), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo user"}]}]}}`)
	st := &State{Schema: SchemaState}
	if _, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "claude", "project", project, InstallOptions{WithConfig: true}); err == nil {
		t.Fatal("expected an MCP conflict")
	}
	plan, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "claude", "project", project, InstallOptions{WithConfig: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if !strings.Contains(string(data), "USER RULES") || !strings.Contains(string(data), "PACKAGE RULES") {
		t.Fatalf("user instructions lost: %s", data)
	}
	mcp, _ := os.ReadFile(filepath.Join(project, ".mcp.json"))
	if !strings.Contains(string(mcp), "new") {
		t.Fatalf("mcp not installed: %s", mcp)
	}
	hooks, _ := os.ReadFile(filepath.Join(project, ".claude", "settings.json"))
	if !strings.Contains(string(hooks), "echo user") || !strings.Contains(string(hooks), "echo new") {
		t.Fatalf("hooks not merged: %s", hooks)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	mcp, _ = os.ReadFile(filepath.Join(project, ".mcp.json"))
	if !strings.Contains(string(mcp), "/bin/true") {
		t.Fatalf("previous mcp value not restored: %s", mcp)
	}
	hooks, _ = os.ReadFile(filepath.Join(project, ".claude", "settings.json"))
	if !strings.Contains(string(hooks), "echo user") || strings.Contains(string(hooks), "echo new") {
		t.Fatalf("hooks not restored: %s", hooks)
	}
	data, _ = os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if strings.Contains(string(data), "PACKAGE RULES") || !strings.Contains(string(data), "USER RULES") {
		t.Fatalf("user instructions not restored: %s", data)
	}
}

func mustLatest(t *testing.T, repo *SetRepo, pkg string) VersionInfo {
	t.Helper()
	v, err := repo.Latest(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPushRefusesLiteralMCPCredential(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "x\n")
	writeFile(t, filepath.Join(bundle, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","env":{"API_KEY":"literal-secret"}}}}`)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err == nil {
		t.Fatal("expected credential refusal")
	}
	if _, err := repo.Push(bundle, PushOptions{Name: "p", AllowSecrets: true}); err != nil {
		t.Fatal(err)
	}
}

func TestExtractBundleStripsManagedBlocks(t *testing.T) {
	tmp, _, _, _ := setup(t)
	project := filepath.Join(tmp, "live")
	writeFile(t, filepath.Join(project, "CLAUDE.md"), "user\n<!-- also:begin personal/x -->\nother\n<!-- also:end personal/x -->\n")
	writeFile(t, filepath.Join(project, ".claude", "skills", "s", "SKILL.md"), "---\nname: s\ndescription: d\n---\nbody\n")
	writeFile(t, filepath.Join(project, ".claude", "hooks", "h.sh"), "#!/bin/sh\n")
	dest := filepath.Join(tmp, "infra")
	res, err := ExtractBundle(AdoptOptions{Name: "live", Project: project, Harness: "claude", Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(res.BundleDir, "CLAUDE.md"))
	if strings.Contains(string(data), "<!-- also:") || !strings.Contains(string(data), "user") || !strings.Contains(string(data), "other") {
		t.Fatalf("blocks not unwrapped: %s", data)
	}
	if _, err := os.Stat(filepath.Join(res.BundleDir, "skills", "s", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.BundleDir, "hooks", "h.sh")); err != nil {
		t.Fatal(err)
	}
	if res.Files < 3 {
		t.Fatalf("unexpected file count: %d", res.Files)
	}
}

func TestMCPRenderRoundTrip(t *testing.T) {
	servers := map[string]MCPServer{
		"local":  {Transport: "stdio", Command: []string{"npx", "-y", "srv"}, Env: map[string]string{"A": "b"}},
		"remote": {Transport: "http", URL: "https://x", Headers: map[string]string{"H": "v"}},
	}
	for _, id := range HarnessIDs() {
		h := MustHarness(id)
		out, warns := RenderMCP(h, nil, servers)
		if len(out) != 2 {
			t.Fatalf("%s: %d renders (%v)", id, len(out), warns)
		}
	}
	// Codex output must remain valid TOML.
	h := MustHarness("codex")
	out, _ := RenderMCP(h, nil, servers)
	var b strings.Builder
	for _, r := range out {
		b.WriteString(r.TOML)
	}
	if _, err := parseTOMLMap([]byte(b.String())); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, b.String())
	}
}

func TestOpenCodeLegacyMCPShape(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "x\n")
	writeFile(t, filepath.Join(bundle, ".mcp.json"), `{"mcpServers":{"x":{"command":"/bin/echo","args":["new"]}}}`)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	pkg, _ := repo.LoadPackage("p", mustLatest(t, repo, "p").Version)
	project := filepath.Join(tmp, "project")
	writeFile(t, filepath.Join(project, "opencode.json"), `{
  "mcp": {
    "x": {"type": "local", "command": ["/bin/true"]},
    "timeout": {"startup": 1000}
  }
}`)
	st := &State{Schema: SchemaState}
	v := mustLatest(t, repo, "p")
	plan, err := BuildPlan(st, repo, pkg, v, "opencode", "project", project, InstallOptions{WithConfig: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(project, "opencode.json"))
	s := string(data)
	if strings.Contains(s, `"servers"`) {
		t.Fatalf("legacy shape must be preserved: %s", s)
	}
	if !strings.Contains(s, `"new"`) || !strings.Contains(s, `"timeout"`) {
		t.Fatalf("legacy mcp not updated: %s", s)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(project, "opencode.json"))
	if !strings.Contains(string(data), "/bin/true") {
		t.Fatalf("previous legacy value not restored: %s", data)
	}
}

func TestRoundTripThroughAdoptLayer(t *testing.T) {
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
	pkg, err := repo.LoadPackage("p", mustLatest(t, repo, "p").Version)
	if err != nil {
		t.Fatal(err)
	}

	p1 := filepath.Join(tmp, "p1")
	if err := os.MkdirAll(p1, 0o755); err != nil {
		t.Fatal(err)
	}
	st := &State{Schema: SchemaState}
	v := mustLatest(t, repo, "p")
	plan, err := BuildPlan(st, repo, pkg, v, "opencode", "project", p1, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}

	infra := filepath.Join(tmp, "infra")
	if _, err := ExtractBundle(AdoptOptions{Name: "rt", Project: p1, Harness: "opencode", Dest: infra}); err != nil {
		t.Fatal(err)
	}
	rtBundle := filepath.Join(infra, "rt")
	if _, err := os.Stat(filepath.Join(rtBundle, ".also-layers", "manifest.json")); err != nil {
		t.Fatalf("layer manifest missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rtBundle, ".also-layers", "claude", "agents", "critic.md")); err != nil {
		t.Fatalf("native agent layer missing: %v", err)
	}
	if _, err := repo.Push(rtBundle, PushOptions{Name: "rt"}); err != nil {
		t.Fatal(err)
	}
	rtPkg, err := repo.LoadPackage("rt", mustLatest(t, repo, "rt").Version)
	if err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(tmp, "p2")
	if err := os.MkdirAll(p2, 0o755); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	plan2, err := BuildPlan(st2, repo, rtPkg, mustLatest(t, repo, "rt"), "claude", "project", p2, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st2, plan2, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}
	gotAgent, err := os.ReadFile(filepath.Join(p2, ".claude", "agents", "critic.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantAgent, _ := os.ReadFile(filepath.Join(bundle, "agents", "critic.md"))
	if string(gotAgent) != string(wantAgent) {
		t.Fatalf("native agent not restored:\n--- want\n%s\n--- got\n%s", wantAgent, gotAgent)
	}
	if _, err := os.Stat(filepath.Join(p2, ".claude", "status_lines", "line.py")); err != nil {
		t.Fatalf("native extra not restored: %v", err)
	}
	settings, err := os.ReadFile(filepath.Join(p2, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bash(ls:*)", "statusLine", "PreToolUse"} {
		if !strings.Contains(string(settings), want) {
			t.Fatalf("settings key %q missing:\n%s", want, settings)
		}
	}
	if _, err := os.Stat(filepath.Join(p2, "CLAUDE.md")); err != nil {
		t.Fatalf("instructions missing: %v", err)
	}
}

func TestSettingsAndExtrasSameHarness(t *testing.T) {
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
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	st := &State{Schema: SchemaState}
	plan, err := BuildPlan(st, repo, pkg, mustLatest(t, repo, "p"), "claude", "project", proj, InstallOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlan(st, plan, InstallOptions{WithConfig: true}); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(filepath.Join(proj, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bash(ls:*)", "statusLine", "PreToolUse", "echo status"} {
		if !strings.Contains(string(settings), want) {
			t.Fatalf("settings key %q missing:\n%s", want, settings)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "status_lines", "line.py")); err != nil {
		t.Fatalf("extra missing: %v", err)
	}
	st, _ = LoadState()
	if _, err := Uninstall(st, st.InstallsOf("p")[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "settings.json")); err == nil {
		t.Fatal("settings file created by also should be removed")
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "status_lines")); err == nil {
		t.Fatal("extras should be removed")
	}
}
