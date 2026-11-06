package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	if _, err := runGit(dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	args := append(gitCommitArgs(dir), "commit", "--quiet", "-m", message)
	if _, err := runGit(dir, args...); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeKeepsInstallWhenSelectedPartDisappears(t *testing.T) {
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
	first, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "opencode", Scope: "project", Project: project,
		Parts: []string{"skill:review"}, WithConfig: true,
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := first.Install.Version

	if err := os.RemoveAll(filepath.Join(bundle, "skills", "review")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "skills", "other", "SKILL.md"), "---\nname: other\ndescription: d\n---\nother\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	results, err := UpgradeInstalls(cfg, st, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("upgrade with a vanished part must fail, got %+v", results)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("old selected part was removed: %v", err)
	}
	st, _ = LoadState()
	if got := st.InstallsOf("p")[0].Version; got != oldVersion {
		t.Fatalf("state moved to %s after a failed upgrade", got)
	}
}

func TestTomlEditIsNotRemovedWithoutForce(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(bundle, "config.toml"), "[mcp_servers.demo]\ncommand = \"/bin/echo\"\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "codex"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "codex", Scope: "project", Project: project, WithConfig: true,
	}, "", ""); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(project, ".codex", "config.toml")
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(strings.ReplaceAll(string(data), "/bin/echo", "/bin/EVIL")), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := Uninstall(st, st.InstallsOf("p")[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "left modified config entry") {
		t.Fatalf("modified TOML edit was not reported: %v", warnings)
	}
	data, err = os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/bin/EVIL") || !strings.Contains(string(data), "# also:begin mcp:demo") {
		t.Fatalf("modified TOML block was removed:\n%s", data)
	}
}

func TestInstallSamePackageInTwoHarnesses(t *testing.T) {
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
	for _, harness := range []string{"claude", "opencode"} {
		if _, _, err := InstallPackage(cfg, InstallRequest{
			Package: "p", Harness: harness, Scope: "project", Project: project, WithConfig: true,
		}, "", ""); err != nil {
			t.Fatalf("install for %s: %v", harness, err)
		}
	}
	for _, rel := range []string{
		"CLAUDE.md",
		filepath.Join(".claude", "skills", "review", "SKILL.md"),
		"AGENTS.md",
		filepath.Join(".opencode", "skills", "review", "SKILL.md"),
	} {
		if _, err := os.Stat(filepath.Join(project, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installs := st.InstallsOf("p")
	if len(installs) != 2 {
		t.Fatalf("expected two harness slots, got %d: %+v", len(installs), installs)
	}
	got := map[string]bool{}
	for _, in := range installs {
		got[in.Harness] = true
	}
	if !got["claude"] || !got["opencode"] {
		t.Fatalf("wrong harness slots: %+v", got)
	}
}

func TestPushPublishesLegacyVersionTags(t *testing.T) {
	tmp, remote, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	snap, _, err := ReadSnapshot(bundle, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude", Label: "v1"}
	meta.Digest = snap.Digest(meta)
	legacyVersion := VersionFromDigest(meta.Digest)
	if err := WritePackage(filepath.Join(setDir, "packages", "p", legacyVersion), meta, snap); err != nil {
		t.Fatal(err)
	}
	commitAll(t, setDir, "legacy v1")
	if _, err := runGit(setDir, "push", "--quiet", "--set-upstream", "origin", "HEAD"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "two\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(remote, "tag", "--list", versionTagName("p", legacyVersion)); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("legacy tag was not pushed: %q %v", out, err)
	}
}

func TestVersionsRejectsMalformedDigest(t *testing.T) {
	tmp, _, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	snap, _, err := ReadSnapshot(bundle, SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	meta := PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "claude", Digest: "x"}
	if err := WritePackage(filepath.Join(setDir, "packages", "p"), meta, snap); err != nil {
		t.Fatal(err)
	}
	commitAll(t, setDir, "broken manifest")
	if _, err := repo.Versions("p"); err == nil || !strings.Contains(err.Error(), "malformed digest") {
		t.Fatalf("expected malformed digest error, got %v", err)
	}
}

func TestOpenCodeLegacyMCPIgnoresNonServerKeys(t *testing.T) {
	files := map[string]*SnapshotFile{
		"opencode.json": {Data: []byte(`{"mcp":{"x":{"type":"local","command":["/bin/echo"]},"timeout":{"startup":1000}}}`), Mode: 0o644},
	}
	servers, warnings, err := ExtractBundleMCP(MustHarness("opencode"), files, []string{""})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers["x"].Transport != "stdio" {
		t.Fatalf("unexpected servers: %+v", servers)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "timeout") {
		t.Fatalf("missing non-server warning: %v", warnings)
	}
}

func TestMCPFirstConfigFallsThrough(t *testing.T) {
	files := map[string]*SnapshotFile{
		".mcp.json":     {Data: []byte(`{}`), Mode: 0o644},
		"settings.json": {Data: []byte(`{"mcpServers":{"fromsettings":{"command":"/bin/echo"}}}`), Mode: 0o644},
	}
	servers, _, err := ExtractBundleMCP(MustHarness("claude"), files, []string{""})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers["fromsettings"].Transport != "stdio" {
		t.Fatalf("later MCP config was ignored: %+v", servers)
	}
}

func TestMCPSecretsInArgsAndURL(t *testing.T) {
	servers := map[string]MCPServer{
		"args": {Transport: "stdio", Command: []string{"tool", "--token", "sk-live"}},
		"url":  {Transport: "http", URL: "https://user:pass@example.com/mcp?api_key=sk-live"},
	}
	got := strings.Join(MCPSuspiciousSecrets(servers), "\n")
	if !strings.Contains(got, "args") {
		t.Fatalf("command argument secret not detected: %s", got)
	}
	if !strings.Contains(got, "url") {
		t.Fatalf("URL secret not detected: %s", got)
	}
}

func TestSkillReadmeIsNotASkill(t *testing.T) {
	pkg := &Package{Files: map[string]*SnapshotFile{
		"skills/README.md": {Data: []byte("# docs\n"), Mode: 0o644},
	}}
	parts, err := ClassifyPackage(pkg, MustHarness("claude"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts.Skills) != 0 {
		t.Fatalf("README was classified as a skill: %+v", parts.Skills)
	}
	if !contains(parts.Other, "skills/README.md") {
		t.Fatalf("README was not kept as an ordinary file: %v", parts.Other)
	}
}

func TestOfflineRefusesMissingClone(t *testing.T) {
	_, remote, _, _ := setup(t)
	if _, err := openRemote(remote, false, true); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline error, got %v", err)
	}
}

func TestPushRestoresCloneWhenCommitFails(t *testing.T) {
	tmp, _, setDir, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	sampleBundle(t, bundle)
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(setDir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), "two\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p"}); err == nil {
		t.Fatal("expected commit failure")
	}
	dirty, status, err := gitDirty(setDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatalf("set clone left dirty after failed push:\n%s", status)
	}
	versions, err := repo.Versions("p")
	if err != nil || len(versions) != 1 {
		t.Fatalf("failed push changed versions: %+v %v", versions, err)
	}
}
