package also

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pushClaudeSettingsBundle(t *testing.T, cfg *Config, tmp, name, settings string) {
	t.Helper()
	pushClaudeSettingsVersion(t, cfg, tmp, name, "rules\n", settings)
}

func pushClaudeSettingsVersion(t *testing.T, cfg *Config, tmp, name, body, settings string) {
	t.Helper()
	bundle := filepath.Join(tmp, "bundle-"+name)
	writeFile(t, filepath.Join(bundle, "CLAUDE.md"), body)
	writeFile(t, filepath.Join(bundle, "settings.json"), settings)
	repo := openRepo(t, cfg)
	if _, err := repo.Push(bundle, PushOptions{Name: name, Harness: "claude"}); err != nil {
		t.Fatalf("push %s: %v", name, err)
	}
}

func upgradeSettingsPackage(t *testing.T, cfg *Config, project, name string) []UpgradeResult {
	t.Helper()
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	results, err := UpgradeInstallsInProject(cfg, st, project, []string{name}, false, false, false)
	if err != nil {
		t.Fatalf("upgrade %s: %v", name, err)
	}
	if len(results) != 1 || results[0].Err != nil || results[0].Status != "upgraded" {
		t.Fatalf("upgrade did not converge: %+v", results)
	}
	return results
}

func installSettingsPackage(t *testing.T, cfg *Config, project, pkg, harness string, force bool) *Plan {
	t.Helper()
	plan, _, err := InstallPackage(cfg, InstallRequest{
		Package: pkg, Harness: harness, Scope: "project", Project: project,
		WithConfig: true, Force: force,
	}, "", "")
	if err != nil {
		t.Fatalf("install %s into %s: %v", pkg, harness, err)
	}
	return plan
}

func uninstallPlan(t *testing.T, plan *Plan) {
	t.Helper()
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	in := st.FindByID(plan.Install.ID)
	if in == nil {
		t.Fatalf("install %s disappeared from state", plan.Install.ID)
	}
	if _, err := Uninstall(st, in, false); err != nil {
		t.Fatal(err)
	}
}

func decodedPermissions(t *testing.T, file string) map[string][]string {
	t.Helper()
	doc, err := ParseJSONC(mustReadTestFile(t, file))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permissions")
	if !ok {
		t.Fatalf("%s has no permissions", file)
	}
	var perms map[string][]string
	if err := json.Unmarshal(raw, &perms); err != nil {
		t.Fatalf("permissions in %s: %v", file, err)
	}
	return perms
}

func decodedPermission(t *testing.T, file string) map[string]any {
	t.Helper()
	doc, err := ParseJSONC(mustReadTestFile(t, file))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permission")
	if !ok {
		return map[string]any{}
	}
	var perms map[string]any
	if err := json.Unmarshal(raw, &perms); err != nil {
		t.Fatalf("permission in %s: %v", file, err)
	}
	return perms
}

func claudeSettingsIntoOpenCode(t *testing.T, sourceSettings, existing string, force bool) (project string, plan *Plan) {
	t.Helper()
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsBundle(t, cfg, tmp, "p", sourceSettings)
	project = filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if existing != "" {
		writeFile(t, filepath.Join(project, "opencode.json"), existing)
	}
	plan = installSettingsPackage(t, cfg, project, "p", "opencode", force)
	return project, plan
}

func TestInstallOpenCodeSettingsIntoClaude(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p",
		`{"permission":{"bash":{"git push":"deny"},"edit":"ask"},"theme":"dark"}`)
	settingsPath := filepath.Join(project, ".claude", "settings.json")
	// Pre-existing user values must survive both the install and the uninstall.
	writeFile(t, settingsPath, `{"permissions":{"deny":["Read"]},"custom":"keep"}`)
	plan := installSettingsPackage(t, cfg, project, "p", "claude", false)

	perms := decodedPermissions(t, settingsPath)
	assertStringSet(t, "deny", perms["deny"], "Bash", "Read")
	assertStringSet(t, "ask", perms["ask"], "Edit", "NotebookEdit", "Write")
	doc, err := ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	rawTheme, ok := doc.RawAt("theme")
	if !ok || string(rawTheme) != `"dark"` {
		t.Fatalf("theme = %s, want dark", rawTheme)
	}
	if raw, ok := doc.RawAt("custom"); !ok || string(raw) != `"keep"` {
		t.Fatalf("custom user key was not preserved: %s", raw)
	}
	text := warningsText(plan.Warnings)
	if strings.Contains(text, "was applied") {
		t.Fatalf("translator still claims the value was applied:\n%s", text)
	}
	for _, want := range []string{
		"is scoped; Claude Code cannot express the scope, so the translation covers the whole Bash tool",
		`theme "dark" was translated for Claude Code`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}

	uninstallPlan(t, plan)
	doc, err = ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permissions", "deny")
	if !ok {
		t.Fatal("uninstall removed the pre-existing deny list")
	}
	var deny []string
	if err := json.Unmarshal(raw, &deny); err != nil {
		t.Fatal(err)
	}
	assertStringSet(t, "restored deny", deny, "Read")
	if _, ok := doc.RawAt("theme"); ok {
		t.Fatalf("uninstall left the theme in %s", settingsPath)
	}
	if raw, ok := doc.RawAt("custom"); !ok || string(raw) != `"keep"` {
		t.Fatalf("uninstall dropped the custom user key: %s", raw)
	}
}

func TestInstallClaudeSettingsIntoOpenCode(t *testing.T) {
	project, plan := claudeSettingsIntoOpenCode(t,
		`{"permissions":{"allow":["Read"],"ask":["Edit"],"deny":["Bash"]}}`,
		`{"permission":{"read":"allow"}}`, false)

	configPath := filepath.Join(project, "opencode.json")
	perms := decodedPermission(t, configPath)
	want := map[string]any{"read": "allow", "edit": "ask", "bash": "deny"}
	if !reflect.DeepEqual(perms, want) {
		t.Fatalf("permission = %v, want %v", perms, want)
	}
	if strings.Contains(warningsText(plan.Warnings), "permission") {
		t.Fatalf("unexpected permission warning: %v", plan.Warnings)
	}

	uninstallPlan(t, plan)
	perms = decodedPermission(t, configPath)
	if !reflect.DeepEqual(perms, map[string]any{"read": "allow"}) {
		t.Fatalf("uninstall did not restore the pre-existing permission map: %v", perms)
	}
}

func TestInstallClaudeSettingsIntoQwenAppendsPermissions(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsBundle(t, cfg, tmp, "p",
		`{"permissions":{"allow":["Read"],"ask":["Edit"],"deny":["Bash"]}}`)
	project := filepath.Join(tmp, "project")
	settingsPath := filepath.Join(project, ".qwen", "settings.json")
	writeFile(t, settingsPath, `{"permissions":{"deny":["Read"]},"custom":"keep"}`)
	plan := installSettingsPackage(t, cfg, project, "p", "qwen", false)

	perms := decodedPermissions(t, settingsPath)
	assertStringSet(t, "allow", perms["allow"], "Read")
	assertStringSet(t, "ask", perms["ask"], "Edit")
	assertStringSet(t, "deny", perms["deny"], "Read", "Bash")
	doc, err := ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := doc.RawAt("custom"); !ok || string(raw) != `"keep"` {
		t.Fatalf("custom user key was not preserved: %s", raw)
	}

	uninstallPlan(t, plan)
	perms = decodedPermissions(t, settingsPath)
	assertStringSet(t, "restored deny", perms["deny"], "Read")
}

func TestInstallOpenCodeSettingsIntoCodexWarns(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	// OpenCode carries model/permission/theme, so the Codex warning names the
	// keys the package would have installed elsewhere.
	project := publishOpenCodeMCP(t, cfg, tmp, "p",
		`{"permission":{"bash":"deny"},"theme":"dark","model":"opus"}`)
	plan := installSettingsPackage(t, cfg, project, "p", "codex", false)

	if len(plan.Edits) != 0 {
		t.Fatalf("Codex received settings edits: %+v", plan.Edits)
	}
	if _, statErr := os.Stat(filepath.Join(project, "config.toml")); !os.IsNotExist(statErr) {
		t.Fatalf("Codex wrote config.toml for a settings-only package: %v", statErr)
	}
	text := warningsText(plan.Warnings)
	for _, want := range []string{
		"preserved in the package",
		"Codex CLI has no mergeable",
		"model, permission, theme",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
}

func TestCrossHarnessThemeKeepsExistingTargetValue(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"theme":"dark"}`)
	settingsPath := filepath.Join(project, ".claude", "settings.json")
	writeFile(t, settingsPath, `{"theme":"light"}`)

	plan := installSettingsPackage(t, cfg, project, "p", "claude", false)
	doc, err := ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	rawTheme, ok := doc.RawAt("theme")
	if !ok || string(rawTheme) != `"light"` {
		t.Fatalf("theme = %s, want the existing light", rawTheme)
	}
	text := warningsText(plan.Warnings)
	for _, want := range []string{
		`setting "theme" is already configured differently`,
		"kept the existing target value",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}

	uninstallPlan(t, plan)
	doc, err = ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	rawTheme, ok = doc.RawAt("theme")
	if !ok || string(rawTheme) != `"light"` {
		t.Fatalf("uninstall changed the existing theme to %s", rawTheme)
	}
}

func TestCrossHarnessPermissionStrength(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		source   string
		want     string
		wantWarn string
	}{
		{"allow is raised to deny", "allow", `{"permissions":{"deny":["Bash"]}}`, "deny", ""},
		{"existing deny beats allow", "deny", `{"permissions":{"allow":["Bash"]}}`, "deny", `already has the stronger "deny" effect`},
		{"ask is raised to deny", "ask", `{"permissions":{"deny":["Bash"]}}`, "deny", ""},
		{"existing ask beats allow", "ask", `{"permissions":{"allow":["Bash"]}}`, "ask", `already has the stronger "ask" effect`},
		{"existing deny beats ask", "deny", `{"permissions":{"ask":["Bash"]}}`, "deny", `already has the stronger "deny" effect`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, plan := claudeSettingsIntoOpenCode(t, tc.source,
				`{"permission":{"bash":"`+tc.existing+`"}}`, false)
			perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
			if perms["bash"] != tc.want {
				t.Fatalf("permission.bash = %v, want %s", perms["bash"], tc.want)
			}
			if tc.wantWarn != "" && !strings.Contains(warningsText(plan.Warnings), tc.wantWarn) {
				t.Fatalf("missing warning %q:\n%s", tc.wantWarn, warningsText(plan.Warnings))
			}
		})
	}
}

func TestCrossHarnessForceNeverWeakens(t *testing.T) {
	t.Run("existing stronger effect", func(t *testing.T) {
		project, plan := claudeSettingsIntoOpenCode(t, `{"permissions":{"allow":["Bash"]}}`,
			`{"permission":{"bash":"deny"}}`, true)
		perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
		if perms["bash"] != "deny" {
			t.Fatalf("--force weakened permission.bash to %v", perms["bash"])
		}
		if !strings.Contains(warningsText(plan.Warnings), `already has the stronger "deny" effect`) {
			t.Fatalf("missing no-weakening warning: %v", plan.Warnings)
		}
	})

	t.Run("scoped map", func(t *testing.T) {
		project, plan := claudeSettingsIntoOpenCode(t, `{"permissions":{"deny":["Bash"]}}`,
			`{"permission":{"bash":{"git push":"deny"}}}`, true)
		doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := doc.RawAt("permission", "bash")
		if !ok || !strings.Contains(string(raw), "git push") {
			t.Fatalf("--force replaced the scoped map: %s", raw)
		}
		if !strings.Contains(warningsText(plan.Warnings), "keeps a scoped permission map") {
			t.Fatalf("missing scoped-map warning: %v", plan.Warnings)
		}
	})
}

func TestCrossHarnessOpenCodeV2TargetKeepsArray(t *testing.T) {
	existing := `{"permissions":[{"action":"bash","effect":"allow"}]}`
	project, plan := claudeSettingsIntoOpenCode(t, `{"permissions":{"deny":["Bash"]}}`, existing, false)

	configPath := filepath.Join(project, "opencode.json")
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permissions")
	if !ok || !strings.Contains(string(raw), `"action"`) || !strings.Contains(string(raw), `"allow"`) {
		t.Fatalf("V2 permissions array was changed: %s", raw)
	}
	if _, ok := doc.RawAt("permission"); ok {
		t.Fatalf("translated v1 permission was written next to the V2 array")
	}
	if !strings.Contains(warningsText(plan.Warnings), "V2 ordered permissions array") {
		t.Fatalf("missing V2 warning: %v", plan.Warnings)
	}
}

func TestSameHarnessForceReplacesConflictingScalar(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"bash":"deny"}}`)
	configPath := filepath.Join(project, "opencode.json")
	writeFile(t, configPath, `{"permission":{"bash":"allow"}}`)

	_, _, err := InstallPackage(cfg, InstallRequest{
		Package: "p", Harness: "opencode", Scope: "project", Project: project, WithConfig: true,
	}, "", "")
	if err == nil || !strings.Contains(err.Error(), "already configured differently") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("same-harness conflict = %v, want a --force error", err)
	}

	installSettingsPackage(t, cfg, project, "p", "opencode", true)
	perms := decodedPermission(t, configPath)
	if perms["bash"] != "deny" {
		t.Fatalf("same-harness --force left permission.bash = %v, want deny", perms["bash"])
	}
}

func TestSameHarnessClaudeDoesNotInstallTheme(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsBundle(t, cfg, tmp, "p",
		`{"theme":"dark","permissions":{"allow":["Read"]}}`)
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	installSettingsPackage(t, cfg, project, "p", "claude", false)

	settingsPath := filepath.Join(project, ".claude", "settings.json")
	doc, err := ParseJSONC(mustReadTestFile(t, settingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.RawAt("theme"); ok {
		t.Fatalf("same-harness Claude installed a package theme: %s", mustReadTestFile(t, settingsPath))
	}
	perms := decodedPermissions(t, settingsPath)
	assertStringSet(t, "allow", perms["allow"], "Read")
}

func TestCrossHarnessScopedPermissionMapIsKept(t *testing.T) {
	project, plan := claudeSettingsIntoOpenCode(t, `{"permissions":{"deny":["Bash"]}}`,
		`{"permission":{"bash":{"git push":"deny"}}}`, false)
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permission", "bash")
	if !ok || !strings.Contains(string(raw), "git push") {
		t.Fatalf("translated effect replaced the scoped map: %s", raw)
	}
	if !strings.Contains(warningsText(plan.Warnings), "keeps a scoped permission map") {
		t.Fatalf("missing scoped-map warning: %v", plan.Warnings)
	}
}

func TestCrossHarnessWildcardPermissionFallback(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		existing string
		wantBash string
		wantStar string
		wantWarn string
	}{
		{
			name:     "wildcard deny beats translated allow",
			source:   `{"permissions":{"allow":["Bash"]}}`,
			existing: `{"permission":{"*":"deny"}}`,
			wantBash: "deny",
			wantStar: "deny",
			wantWarn: `already has the stronger "deny" effect`,
		},
		{
			name:     "wildcard ask beats translated allow",
			source:   `{"permissions":{"allow":["Bash"]}}`,
			existing: `{"permission":{"*":"ask"}}`,
			wantBash: "ask",
			wantStar: "ask",
			wantWarn: `already has the stronger "ask" effect`,
		},
		{
			name:     "specific allow is raised by translated wildcard deny",
			source:   `{"permissions":{"deny":["*"]}}`,
			existing: `{"permission":{"*":"deny","bash":"allow"}}`,
			wantBash: "deny",
			wantStar: "deny",
		},
		{
			name:     "specific deny blocks translated wildcard allow",
			source:   `{"permissions":{"allow":["*"]}}`,
			existing: `{"permission":{"bash":"deny"}}`,
			wantBash: "deny",
			wantStar: "allow",
			wantWarn: "was written as the global rule",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, plan := claudeSettingsIntoOpenCode(t, tc.source, tc.existing, false)
			perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
			if perms["bash"] != tc.wantBash {
				t.Fatalf("permission.bash = %v, want %s", perms["bash"], tc.wantBash)
			}
			if star, ok := perms["*"]; ok {
				if star != tc.wantStar {
					t.Fatalf("permission.* = %v, want %s", star, tc.wantStar)
				}
			} else if tc.wantStar != "" {
				t.Fatalf("permission.* disappeared, want %s", tc.wantStar)
			}
			if !strings.Contains(warningsText(plan.Warnings), tc.wantWarn) {
				t.Fatalf("missing warning %q:\n%s", tc.wantWarn, warningsText(plan.Warnings))
			}
		})
	}
}

func TestUpgradeRetainsReconciledPermissionValues(t *testing.T) {
	t.Run("deny survives an allow upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")
		if got := decodedPermission(t, configPath)["bash"]; got != "deny" {
			t.Fatalf("v1 permission.bash = %v, want deny", got)
		}

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		if got := decodedPermission(t, configPath)["bash"]; got != "deny" {
			t.Fatalf("upgrade weakened permission.bash to %v", got)
		}
	})

	t.Run("ask survives an allow upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"ask":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		configPath := filepath.Join(project, "opencode.json")
		if got := decodedPermission(t, configPath)["bash"]; got != "ask" {
			t.Fatalf("upgrade weakened permission.bash to %v", got)
		}
	})

	t.Run("scoped map survives an upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")
		writeFile(t, configPath, `{"permission":{"bash":{"git push":"deny"}}}`)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		doc, err := ParseJSONC(mustReadTestFile(t, configPath))
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := doc.RawAt("permission", "bash")
		if !ok || !strings.Contains(string(raw), "git push") {
			t.Fatalf("upgrade dropped the scoped map: %s", raw)
		}
	})

	t.Run("theme conflict survives an upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"theme":"dark"}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")
		writeFile(t, configPath, `{"theme":"light"}`)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"theme":"dark"}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		doc, err := ParseJSONC(mustReadTestFile(t, configPath))
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := doc.RawAt("theme")
		if !ok || string(raw) != `"light"` {
			t.Fatalf("upgrade changed the user theme to %s", raw)
		}
	})

	t.Run("V2 target array survives an upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")
		// The user switched to the V2 array but the package-created v1 value is
		// still there; the upgrade must not delete it.
		writeFile(t, configPath, `{"permission":{"bash":"deny"},"permissions":[{"action":"bash","effect":"allow"}]}`)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		doc, err := ParseJSONC(mustReadTestFile(t, configPath))
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := doc.RawAt("permissions")
		if !ok || !strings.Contains(string(raw), `"action"`) || !strings.Contains(string(raw), `"allow"`) {
			t.Fatalf("upgrade changed the V2 array: %s", raw)
		}
		raw, ok = doc.RawAt("permission", "bash")
		if !ok || string(raw) != `"deny"` {
			t.Fatalf("upgrade deleted the package-created v1 value: %s", raw)
		}
	})
}

func TestClaudeThemeReachesOpenCodeCrossHarness(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsBundle(t, cfg, tmp, "p",
		`{"theme":"dark","permissions":{"allow":["Read"]}}`)
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := installSettingsPackage(t, cfg, project, "p", "opencode", false)

	configPath := filepath.Join(project, "opencode.json")
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("theme")
	if !ok || string(raw) != `"dark"` {
		t.Fatalf("Claude theme did not reach OpenCode: %s", raw)
	}
	if perms := decodedPermission(t, configPath); perms["read"] != "allow" {
		t.Fatalf("permission.read = %v, want allow", perms["read"])
	}
	if !strings.Contains(warningsText(plan.Warnings), `theme "dark" was translated for OpenCode`) {
		t.Fatalf("missing theme warning: %v", plan.Warnings)
	}
}

func TestQwenTranslatedPermissionsNeedNoForce(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsBundle(t, cfg, tmp, "p",
		`{"permissions":{"allow":["Read"],"deny":["Bash"]}}`)
	project := filepath.Join(tmp, "project")
	settingsPath := filepath.Join(project, ".qwen", "settings.json")
	writeFile(t, settingsPath, `{"permissions":{"deny":["Read"]}}`)

	plan := installSettingsPackage(t, cfg, project, "p", "qwen", false)
	perms := decodedPermissions(t, settingsPath)
	assertStringSet(t, "allow", perms["allow"], "Read")
	assertStringSet(t, "deny", perms["deny"], "Read", "Bash")
	if strings.Contains(warningsText(plan.Warnings), "--force") {
		t.Fatalf("Qwen install demanded --force: %v", plan.Warnings)
	}

	// A second push appends the new rules on upgrade without a force gate.
	pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"ask":["Edit"]}}`)
	upgradeSettingsPackage(t, cfg, project, "p")
	perms = decodedPermissions(t, settingsPath)
	assertStringSet(t, "ask", perms["ask"], "Edit")
}

func pushQwenSettingsVersion(t *testing.T, cfg *Config, tmp, name, body, settings string) {
	t.Helper()
	bundle := filepath.Join(tmp, "bundle-"+name)
	writeFile(t, filepath.Join(bundle, "QWEN.md"), body)
	writeFile(t, filepath.Join(bundle, "settings.json"), settings)
	repo := openRepo(t, cfg)
	if _, err := repo.Push(bundle, PushOptions{Name: name, Harness: "qwen"}); err != nil {
		t.Fatalf("push %s: %v", name, err)
	}
}

func TestUpgradeRetainsPermissionArraysAcrossHarnesses(t *testing.T) {
	t.Run("Claude deny upgrade into Qwen", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "qwen", false)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		settingsPath := filepath.Join(project, ".qwen", "settings.json")
		if _, err := os.Stat(settingsPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		perms := decodedPermissions(t, settingsPath)
		assertStringSet(t, "deny", perms["deny"], "Bash")
	})

	t.Run("Qwen deny upgrade into Claude", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushQwenSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "claude", false)

		pushQwenSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")
		settingsPath := filepath.Join(project, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err != nil {
			t.Fatalf("upgrade removed the settings file: %v", err)
		}
		perms := decodedPermissions(t, settingsPath)
		assertStringSet(t, "deny", perms["deny"], "Bash")
	})
}

func TestUpgradeKeepsSpecificDenyUnderWildcard(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(project, "opencode.json")
	writeFile(t, configPath, `{"permission":{"*":"allow"}}`)
	installSettingsPackage(t, cfg, project, "p", "opencode", false)
	if got := decodedPermission(t, configPath)["bash"]; got != "deny" {
		t.Fatalf("v1 permission.bash = %v, want deny", got)
	}

	pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["*"]}}`)
	upgradeSettingsPackage(t, cfg, project, "p")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("upgrade removed the settings file: %v", err)
	}
	perms := decodedPermission(t, configPath)
	if perms["bash"] != "deny" {
		t.Fatalf("upgrade dropped the specific deny under the wildcard: %v", perms)
	}
	if perms["*"] != "allow" {
		t.Fatalf("upgrade changed the target wildcard: %v", perms)
	}
}

// An upgrade must not retain an old allow grant that the new plan no longer
// covers. The new ask/deny is written; the stale allow disappears.
func TestUpgradeDropsOldAllowWhenNewPlanHasNoAllow(t *testing.T) {
	t.Run("Claude target", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushQwenSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"allow":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "claude", false)
		settingsPath := filepath.Join(project, ".claude", "settings.json")
		assertStringSet(t, "v1 allow", decodedPermissions(t, settingsPath)["allow"], "Bash")

		pushQwenSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"ask":["Edit"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermissions(t, settingsPath)
		if len(perms["allow"]) != 0 {
			t.Fatalf("upgrade retained the stale Bash allow: %v", perms["allow"])
		}
		assertStringSet(t, "ask", perms["ask"], "Edit")
	})

	t.Run("Qwen target", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"allow":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "qwen", false)
		settingsPath := filepath.Join(project, ".qwen", "settings.json")
		assertStringSet(t, "v1 allow", decodedPermissions(t, settingsPath)["allow"], "Bash")

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"ask":["Edit"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermissions(t, settingsPath)
		if len(perms["allow"]) != 0 {
			t.Fatalf("upgrade retained the stale Bash allow: %v", perms["allow"])
		}
		assertStringSet(t, "ask", perms["ask"], "Edit")
	})

	t.Run("OpenCode target", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"allow":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")
		if got := decodedPermission(t, configPath)["bash"]; got != "allow" {
			t.Fatalf("v1 permission.bash = %v, want allow", got)
		}

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"ask":["Edit"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermission(t, configPath)
		if got, ok := perms["bash"]; ok {
			t.Fatalf("upgrade retained the stale Bash allow: %v", got)
		}
		if perms["edit"] != "ask" {
			t.Fatalf("permission.edit = %v, want ask", perms["edit"])
		}
	})
}

// When the new package still declares a permission key but the translator
// produces nothing, the previous restriction must survive the upgrade and a
// warning must say why.
func TestUpgradeRetainsDeclaredButUntranslatablePermissions(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"bash":"deny"}}`)
	settingsPath := filepath.Join(project, ".claude", "settings.json")
	installSettingsPackage(t, cfg, project, "p", "claude", false)
	assertStringSet(t, "v1 deny", decodedPermissions(t, settingsPath)["deny"], "Bash")

	publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"frobnicate":"deny"}}`)
	upgradeSettingsPackage(t, cfg, project, "p")

	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("upgrade removed the settings file: %v", err)
	}
	perms := decodedPermissions(t, settingsPath)
	assertStringSet(t, "deny", perms["deny"], "Bash")

	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installs := st.InstallsOf("p")
	if len(installs) != 1 {
		t.Fatalf("expected one install, got %d", len(installs))
	}
	text := warningsText(installs[0].Warnings)
	for _, want := range []string{
		"declared permission rules produced no translatable output",
		"previous permission edits were retained",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
}

// The target's specific keys override the translated global rule, so the
// global must still be written without deleting or weakening them.
func TestOpenCodeWildcardWrittenWhenSpecificKeysExist(t *testing.T) {
	for _, effect := range []string{"deny", "ask"} {
		t.Run("specific deny under global "+effect, func(t *testing.T) {
			project, _ := claudeSettingsIntoOpenCode(t,
				`{"permissions":{"`+effect+`":["*"]}}`,
				`{"permission":{"bash":"deny"}}`, false)
			perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
			if perms["*"] != effect {
				t.Fatalf("permission.* = %v, want %s", perms["*"], effect)
			}
			if perms["bash"] != "deny" {
				t.Fatalf("permission.bash = %v, want the specific deny", perms["bash"])
			}
		})
	}

	t.Run("specific allow is raised by global deny", func(t *testing.T) {
		project, _ := claudeSettingsIntoOpenCode(t,
			`{"permissions":{"deny":["*"]}}`,
			`{"permission":{"bash":"allow"}}`, false)
		perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
		if perms["*"] != "deny" {
			t.Fatalf("permission.* = %v, want deny", perms["*"])
		}
		if perms["bash"] != "deny" {
			t.Fatalf("permission.bash = %v, want deny", perms["bash"])
		}
	})
}

func singleInstall(t *testing.T, pkg string) *Install {
	t.Helper()
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installs := st.InstallsOf(pkg)
	if len(installs) != 1 {
		t.Fatalf("expected one install of %s, got %d", pkg, len(installs))
	}
	return installs[0]
}

func installWarningsText(t *testing.T, pkg string) string {
	t.Helper()
	return warningsText(singleInstall(t, pkg).Warnings)
}

// The warning must report what actually happened to the previous permission
// edits, not guess before retention has run.
func TestRetentionWarningReportsWhetherEditsWereKept(t *testing.T) {
	t.Run("allow is removed", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"bash":"allow"}}`)
		settingsPath := filepath.Join(project, ".claude", "settings.json")
		installSettingsPackage(t, cfg, project, "p", "claude", false)
		assertStringSet(t, "v1 allow", decodedPermissions(t, settingsPath)["allow"], "Bash")

		publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"frobnicate":"deny"}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		// The allow grant was the only content, so the file may be removed too.
		if data, err := os.ReadFile(settingsPath); err == nil {
			doc, perr := ParseJSONC(data)
			if perr != nil {
				t.Fatal(perr)
			}
			if raw, ok := doc.RawAt("permissions", "allow"); ok {
				var allow []string
				if err := json.Unmarshal(raw, &allow); err != nil {
					t.Fatalf("parse permissions.allow: %v", err)
				}
				if len(allow) != 0 {
					t.Fatalf("upgrade retained the stale allow: %v", allow)
				}
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		text := installWarningsText(t, "p")
		if !strings.Contains(text, "previous permission edits were removed") {
			t.Fatalf("missing removed warning:\n%s", text)
		}
		if strings.Contains(text, "previous permission edits were retained") {
			t.Fatalf("warning claims edits were retained:\n%s", text)
		}
	})

	t.Run("deny is retained", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"bash":"deny"}}`)
		settingsPath := filepath.Join(project, ".claude", "settings.json")
		installSettingsPackage(t, cfg, project, "p", "claude", false)

		publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"frobnicate":"deny"}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		assertStringSet(t, "deny", decodedPermissions(t, settingsPath)["deny"], "Bash")
		text := installWarningsText(t, "p")
		if !strings.Contains(text, "previous permission edits were retained") {
			t.Fatalf("missing retained warning:\n%s", text)
		}
		if strings.Contains(text, "previous permission edits were removed") {
			t.Fatalf("warning claims edits were removed:\n%s", text)
		}
	})
}

// A first install over a target global string must own the whole permission
// object, not a set of leaves, so the original string is restorable.
func TestFirstInstallOwnsWholePermissionMap(t *testing.T) {
	project, _ := claudeSettingsIntoOpenCode(t,
		`{"permissions":{"deny":["Bash"]}}`,
		`{"permission":"allow"}`, false)

	perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
	want := map[string]any{"*": "allow", "bash": "deny"}
	if !reflect.DeepEqual(perms, want) {
		t.Fatalf("permission = %v, want %v", perms, want)
	}

	in := singleInstall(t, "p")
	if len(in.Edits) != 1 {
		t.Fatalf("expected one config edit, got %+v", in.Edits)
	}
	e := in.Edits[0]
	if e.Kind != "json-set" || !stringSliceEqual(e.Path, []string{"permission"}) {
		t.Fatalf("edit = %s %v, want one whole json-set at permission", e.Kind, e.Path)
	}
	if !e.BaseSet || string(e.Base) != `"allow"` {
		t.Fatalf("edit base = %s (set=%v), want the original allow string", e.Base, e.BaseSet)
	}
}

func TestUpgradeKeepsWholePermissionMapOwnership(t *testing.T) {
	t.Run("whole map upgrade", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(project, "opencode.json")
		writeFile(t, configPath, `{"permission":"allow"}`)
		installSettingsPackage(t, cfg, project, "p", "opencode", false)

		// The user adds an unrelated key to the installed map.
		writeFile(t, configPath, `{"permission":{"*":"allow","bash":"deny","read":"allow"}}`)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["*"],"ask":["Edit"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermission(t, configPath)
		if perms["*"] != "allow" {
			t.Fatalf("permission.* = %v, want allow", perms["*"])
		}
		if perms["edit"] != "ask" {
			t.Fatalf("permission.edit = %v, want ask", perms["edit"])
		}
		if perms["read"] != "allow" {
			t.Fatalf("upgrade dropped the user key: %v", perms)
		}
		// The stale package key was a deny, so it must survive the upgrade.
		if perms["bash"] != "deny" {
			t.Fatalf("upgrade dropped the stale bash deny: %v", perms)
		}

		in := singleInstall(t, "p")
		if len(in.Edits) != 1 {
			t.Fatalf("expected one permission owner, got %+v", in.Edits)
		}
		e := in.Edits[0]
		if e.Kind != "json-set" || !stringSliceEqual(e.Path, []string{"permission"}) {
			t.Fatalf("edit = %s %v, want one whole json-set at permission", e.Kind, e.Path)
		}
		if !e.BaseSet || string(e.Base) != `"allow"` {
			t.Fatalf("edit base = %s (set=%v), want the original allow string", e.Base, e.BaseSet)
		}

		st, err := LoadState()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Uninstall(st, in, false); err != nil {
			t.Fatal(err)
		}
		doc, err := ParseJSONC(mustReadTestFile(t, configPath))
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := doc.RawAt("permission")
		if !ok {
			t.Fatal("uninstall removed the permission key")
		}
		var global string
		if err := json.Unmarshal(raw, &global); err != nil || global != "allow" {
			t.Fatalf("uninstall left permission = %s, want the original allow string", raw)
		}
	})

	t.Run("old leaf edits are covered", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		installSettingsPackage(t, cfg, project, "p", "opencode", false)
		configPath := filepath.Join(project, "opencode.json")

		// The first install ran without a target config, so it owns a leaf.
		in := singleInstall(t, "p")
		if len(in.Edits) != 1 || !stringSliceEqual(in.Edits[0].Path, []string{"permission", "bash"}) {
			t.Fatalf("expected one leaf edit, got %+v", in.Edits)
		}

		// The target gains a global string before the next upgrade.
		writeFile(t, configPath, `{"permission":"allow"}`)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"allow":["*"],"deny":["Bash"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermission(t, configPath)
		want := map[string]any{"*": "allow", "bash": "deny"}
		if !reflect.DeepEqual(perms, want) {
			t.Fatalf("permission = %v, want %v", perms, want)
		}

		in = singleInstall(t, "p")
		if len(in.Edits) != 1 || !stringSliceEqual(in.Edits[0].Path, []string{"permission"}) {
			t.Fatalf("old leaf edits were not folded into one owner: %+v", in.Edits)
		}
	})

	t.Run("user global ask is kept without a v2 star", func(t *testing.T) {
		configPath := wholeMapUpgrade(t,
			`{"permissions":{"deny":["Bash"]}}`,
			`{"permissions":{"ask":["Edit"]}}`,
			`{"permission":{"*":"ask","bash":"deny"}}`)
		perms := decodedPermission(t, configPath)
		if perms["*"] != "ask" {
			t.Fatalf("permission.* = %v, want the user ask", perms["*"])
		}
		assertSingleWholePermissionEdit(t, "p")
		assertUninstallRestoresPermissionString(t, configPath, "allow")
	})

	t.Run("user global deny is kept without a v2 star", func(t *testing.T) {
		configPath := wholeMapUpgrade(t,
			`{"permissions":{"deny":["Bash"]}}`,
			`{"permissions":{"ask":["Edit"]}}`,
			`{"permission":{"*":"deny","bash":"deny"}}`)
		perms := decodedPermission(t, configPath)
		if perms["*"] != "deny" {
			t.Fatalf("permission.* = %v, want the user deny", perms["*"])
		}
		assertSingleWholePermissionEdit(t, "p")
	})

	t.Run("user-changed stale key is kept with a warning", func(t *testing.T) {
		configPath := wholeMapUpgrade(t,
			`{"permissions":{"deny":["Bash"]}}`,
			`{"permissions":{"ask":["Edit"]}}`,
			`{"permission":{"*":"allow","bash":"ask"}}`)
		perms := decodedPermission(t, configPath)
		if perms["bash"] != "ask" {
			t.Fatalf("permission.bash = %v, want the user ask", perms["bash"])
		}
		text := installWarningsText(t, "p")
		if !strings.Contains(text, `permission key "bash"`) || !strings.Contains(text, "was changed by you; the existing value was kept") {
			t.Fatalf("missing user-change warning:\n%s", text)
		}
		assertSingleWholePermissionEdit(t, "p")
	})

	t.Run("stale package allow is dropped", func(t *testing.T) {
		tmp, _, _, cfg := setup(t)
		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", `{"permissions":{"deny":["Bash"]}}`)
		project := filepath.Join(tmp, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(project, "opencode.json")
		writeFile(t, configPath, `{"permission":"allow"}`)
		installSettingsPackage(t, cfg, project, "p", "opencode", false)

		// Simulate an older package that granted Bash: the target and the
		// recorded package value both hold the allow grant.
		writeFile(t, configPath, `{"permission":{"*":"allow","bash":"allow"}}`)
		setStateWholePermissionValue(t, "p", `{"*":"allow","bash":"allow"}`)

		pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", `{"permissions":{"ask":["Edit"]}}`)
		upgradeSettingsPackage(t, cfg, project, "p")

		perms := decodedPermission(t, configPath)
		if _, ok := perms["bash"]; ok {
			t.Fatalf("upgrade kept the stale allow: %v", perms)
		}
		if perms["*"] != "allow" {
			t.Fatalf("permission.* = %v, want allow", perms["*"])
		}
		if perms["edit"] != "ask" {
			t.Fatalf("permission.edit = %v, want ask", perms["edit"])
		}
		assertSingleWholePermissionEdit(t, "p")
	})

	t.Run("user-only keys survive", func(t *testing.T) {
		configPath := wholeMapUpgrade(t,
			`{"permissions":{"deny":["Bash"]}}`,
			`{"permissions":{"ask":["Edit"]}}`,
			`{"permission":{"*":"allow","bash":"deny","read":"allow"}}`)
		perms := decodedPermission(t, configPath)
		if perms["read"] != "allow" {
			t.Fatalf("upgrade dropped the user-only key: %v", perms)
		}
		if perms["bash"] != "deny" {
			t.Fatalf("permission.bash = %v, want the stale deny", perms["bash"])
		}
		assertSingleWholePermissionEdit(t, "p")
	})
}

// wholeMapUpgrade installs v1 over a target global string, replaces the target
// with userConfig when non-empty, then upgrades to v2. It returns the config.
func wholeMapUpgrade(t *testing.T, v1, v2, userConfig string) string {
	t.Helper()
	tmp, _, _, cfg := setup(t)
	pushClaudeSettingsVersion(t, cfg, tmp, "p", "v1\n", v1)
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(project, "opencode.json")
	writeFile(t, configPath, `{"permission":"allow"}`)
	installSettingsPackage(t, cfg, project, "p", "opencode", false)
	if userConfig != "" {
		writeFile(t, configPath, userConfig)
	}
	pushClaudeSettingsVersion(t, cfg, tmp, "p", "v2\n", v2)
	upgradeSettingsPackage(t, cfg, project, "p")
	return configPath
}

func assertSingleWholePermissionEdit(t *testing.T, pkg string) {
	t.Helper()
	in := singleInstall(t, pkg)
	if len(in.Edits) != 1 {
		t.Fatalf("expected one permission owner, got %+v", in.Edits)
	}
	e := in.Edits[0]
	if e.Kind != "json-set" || !stringSliceEqual(e.Path, []string{"permission"}) {
		t.Fatalf("edit = %s %v, want one whole json-set at permission", e.Kind, e.Path)
	}
}

func setStateWholePermissionValue(t *testing.T, pkg, value string) {
	t.Helper()
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	in := st.InstallsOf(pkg)
	if len(in) != 1 {
		t.Fatalf("expected one install of %s, got %d", pkg, len(in))
	}
	found := false
	for i := range in[0].Edits {
		if stringSliceEqual(in[0].Edits[i].Path, []string{"permission"}) {
			in[0].Edits[i].Value = json.RawMessage(value)
			found = true
		}
	}
	if !found {
		t.Fatalf("install %s has no whole-permission edit: %+v", pkg, in[0].Edits)
	}
	if err := SaveState(st); err != nil {
		t.Fatal(err)
	}
}

func assertUninstallRestoresPermissionString(t *testing.T, configPath, want string) {
	t.Helper()
	in := singleInstall(t, "p")
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(st, in, false); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permission")
	if !ok {
		t.Fatal("uninstall removed the permission key")
	}
	var global string
	if err := json.Unmarshal(raw, &global); err != nil || global != want {
		t.Fatalf("uninstall left permission = %s, want the original %q string", raw, want)
	}
}

func TestRetentionWarningSaysNoPreviousEditsExist(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permission":{"frobnicate":"deny"}}`)
	plan := installSettingsPackage(t, cfg, project, "p", "claude", false)
	if len(plan.Edits) != 0 {
		t.Fatalf("untranslatable permission package wrote edits: %+v", plan.Edits)
	}
	text := warningsText(plan.Warnings)
	for _, want := range []string{
		"declared permission rules produced no translatable output",
		"no previous permission edits existed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing warning %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"previous permission edits were retained",
		"previous permission edits were removed",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("fresh install used an upgrade warning %q:\n%s", forbidden, text)
		}
	}
}

// A target global permission string cannot express per-action overrides. A
// stronger translated rule turns it into a map that keeps the global default;
// a weaker translated allow leaves the string untouched.
func TestOpenCodeGlobalStringStrengthensToMap(t *testing.T) {
	for _, effect := range []string{"deny", "ask"} {
		t.Run(effect, func(t *testing.T) {
			project, _ := claudeSettingsIntoOpenCode(t,
				`{"permissions":{"`+effect+`":["Bash"]}}`,
				`{"permission":"allow"}`, false)
			perms := decodedPermission(t, filepath.Join(project, "opencode.json"))
			if perms["*"] != "allow" {
				t.Fatalf("permission.* = %v, want allow", perms["*"])
			}
			if perms["bash"] != effect {
				t.Fatalf("permission.bash = %v, want %s", perms["bash"], effect)
			}
		})
	}
}

func TestOpenCodeGlobalStringStaysWhenTranslatedIsWeaker(t *testing.T) {
	project, _ := claudeSettingsIntoOpenCode(t,
		`{"permissions":{"allow":["Bash"]}}`,
		`{"permission":"allow"}`, false)
	doc, err := ParseJSONC(mustReadTestFile(t, filepath.Join(project, "opencode.json")))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permission")
	if !ok {
		t.Fatal("permission disappeared")
	}
	var global string
	if err := json.Unmarshal(raw, &global); err != nil || global != "allow" {
		t.Fatalf("permission = %s, want the global allow string", raw)
	}
}

func TestOpenCodeGlobalPermissionStringKeepsDeny(t *testing.T) {
	project, _ := claudeSettingsIntoOpenCode(t, `{"permissions":{"allow":["Bash"]}}`,
		`{"permission":"deny"}`, false)
	configPath := filepath.Join(project, "opencode.json")
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("permission")
	if !ok {
		t.Fatal("permission disappeared")
	}
	var global string
	if err := json.Unmarshal(raw, &global); err != nil || global != "deny" {
		t.Fatalf("permission = %s, want the global deny string", raw)
	}
}
