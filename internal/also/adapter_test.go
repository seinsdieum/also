package also

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Never load the developer's real declarative adapters into tests.
	os.Setenv("ALSO_ADAPTERS", filepath.Join(os.TempDir(), "also-test-no-adapters"))
	os.Exit(m.Run())
}

func TestAdaptersAreValid(t *testing.T) {
	list := Harnesses()
	if len(list) < 4 {
		t.Fatalf("expected built-in adapters, got %d", len(list))
	}
	if err := ValidateAdapters(list); err != nil {
		t.Fatal(err)
	}
	for _, h := range list {
		if h.Display == "" || h.Bundle.Instr == "" {
			t.Fatalf("adapter %s is missing required metadata", h.ID)
		}
	}
}

func TestAdapterTargetsStayInsideRoot(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, h := range Harnesses() {
		for _, tc := range []struct {
			scope string
			root  string
		}{{"project", project}, {"global", home}} {
			target := h.TargetPath(tc.scope, project)
			for _, p := range []string{
				target.Instr, target.Skills, target.Commands, target.Agents,
				target.Plugins, target.Hooks, target.Settings, target.HooksJSON, target.MCP,
			} {
				if p == "" {
					continue
				}
				if !isWithin(tc.root, p) {
					t.Fatalf("%s %s path escapes %s: %s", h.ID, tc.scope, tc.root, p)
				}
			}
		}
	}
}

func TestAdapterValidationRejectsUnsafeSpecs(t *testing.T) {
	base := *MustHarness("claude")
	cases := map[string]func(*Harness){
		"traversal skill": func(h *Harness) { h.Project.Skills = "../outside" },
		"absolute project path": func(h *Harness) {
			h.Project.Commands = "/etc/commands"
		},
		"absolute global path": func(h *Harness) { h.Global.MCP = "/etc/mcp.json" },
		"config traversal":     func(h *Harness) { h.MCP.Configs = []string{"../settings.json"} },
		"reserved project dir": func(h *Harness) { h.Project.Hooks = ".git/hooks" },
		"reserved global dir":  func(h *Harness) { h.Global.Skills = "~/.ssh/keys" },
		"unknown mcp format":   func(h *Harness) { h.MCP.Format = "made-up" },
		"hooks without events": func(h *Harness) { h.Hooks.Events = nil },
		"empty display":        func(h *Harness) { h.Display = " " },
		"uppercase alias":      func(h *Harness) { h.Aliases = []string{"Acme"} },
	}
	for name, mutate := range cases {
		h := base
		h.Aliases = append([]string{}, base.Aliases...)
		h.MCP.Configs = append([]string{}, base.MCP.Configs...)
		h.Hooks.Configs = append([]string{}, base.Hooks.Configs...)
		mutate(&h)
		if err := ValidateAdapters([]*Harness{&h}); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestAdapterNamesAreUnique(t *testing.T) {
	a := *MustHarness("claude")
	b := *MustHarness("codex")
	b.Aliases = append(append([]string{}, b.Aliases...), "cc")
	if err := ValidateAdapters([]*Harness{&a, &b}); err == nil {
		t.Fatal("expected duplicate alias to be rejected")
	}
}

func TestAdapterReusesExistingFormats(t *testing.T) {
	// A community adapter for a Claude-compatible harness needs only a spec:
	// no changes to install/render/mcp/hooks.
	acme := &Harness{
		ID: "acme", Display: "Acme Agent", GlobalBase: "~/.acme",
		Global: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands",
			Agents: "agents", Settings: "settings.json", MCP: "mcp.json",
		},
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".acme/skills", Commands: ".acme/commands",
			Agents: ".acme/agents", Settings: ".acme/settings.json", MCP: ".acme/mcp.json",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands",
			Agents: "agents", Settings: "settings.json", MCP: ".mcp.json",
		},
		Roots: []string{"", ".acme"},
		Hooks: HookAdapter{
			Configs: []string{"settings.json"}, Events: claudeHookEvents,
			Target: func(t Target) (string, string) { return t.Settings, "hooks" },
		},
		MCP:      MCPAdapter{Format: MCPClaudeJSON, Configs: []string{".mcp.json"}},
		Settings: SettingsAdapter{Keys: []string{"permissions"}, Configs: []string{"settings.json"}, Target: func(t Target) string { return t.Settings }},
		Commands: CommandAdapter{Format: CommandClaude},
		Agents:   AgentAdapter{Format: AgentClaude},
		Detect:   []DetectRule{{Paths: []string{".acme"}, Weight: 10}},
	}
	if err := ValidateAdapters([]*Harness{acme}); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	target := acme.TargetPath("project", project)
	if target.Instr != filepath.Join(project, "AGENTS.md") {
		t.Fatalf("unexpected target: %s", target.Instr)
	}
}

func TestCustomComponentRenders(t *testing.T) {
	acme := &Harness{
		ID: "acme", Display: "Acme", GlobalBase: "~/.acme",
		Project: Paths{Instr: "AGENTS.md", MCP: ".acme/mcp.json"},
		Bundle:  Paths{Instr: "AGENTS.md", MCP: ".mcp.json"},
		Roots:   []string{""},
		Components: []Component{{
			Kind: "workflows", BundleDir: "workflows", ProjectDir: ".acme/workflows",
			Extensions: []string{".md"},
		}},
	}
	if err := ValidateAdapters([]*Harness{acme}); err != nil {
		t.Fatal(err)
	}
	pkg := &Package{
		Meta: PackageMeta{Schema: SchemaPkg, Name: "p", SourceHarness: "acme"},
		Files: map[string]*SnapshotFile{
			"AGENTS.md":           {Data: []byte("rules\n"), Mode: 0o644},
			"workflows/deploy.md": {Data: []byte("steps\n"), Mode: 0o644},
			"workflows/notes.txt": {Data: []byte("ignored\n"), Mode: 0o644},
		},
	}
	project := t.TempDir()
	target := acme.TargetPath("project", project)
	plan, err := RenderPackage(pkg, acme, target)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(project, ".acme", "workflows", "deploy.md")
	found := false
	for _, f := range plan.Files {
		if f.Dest == dest && string(f.Data) == "steps\n" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom component was not rendered: %+v", plan.Files)
	}
	parts, err := ClassifyPackage(pkg, acme)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts.Custom["workflows"]) != 1 {
		t.Fatalf("extension filter failed: %+v", parts.Custom)
	}
}

func TestAdapterSpecBuildsValidHarness(t *testing.T) {
	spec := AdapterSpec{
		ID: "acme", Display: "Acme", Aliases: []string{"ac"},
		GlobalBase: "~/.acme",
		Global:     Paths{Instr: "AGENTS.md", Skills: "skills", Commands: "commands", Agents: "agents", MCP: "mcp.json"},
		Project:    Paths{Instr: "AGENTS.md", Skills: ".acme/skills", Commands: ".acme/commands", Agents: ".acme/agents", MCP: ".acme/mcp.json"},
		Bundle:     Paths{Instr: "AGENTS.md", Skills: "skills", Commands: "commands", Agents: "agents", Settings: "settings.json", MCP: ".mcp.json"},
		Roots:      []string{"", ".acme"},
		Hooks: &HookSpec{
			Configs: []string{"settings.json"}, Events: claudeHookEvents,
			Target: "settings",
		},
		MCP:      &MCPSpec{Format: MCPClaudeJSON, Configs: []string{".mcp.json"}},
		Settings: &SettingsSpec{Keys: []string{"permissions"}, Configs: []string{"settings.json"}, Target: "settings"},
		Commands: &CommandAdapter{Format: CommandClaude},
		Agents:   &AgentAdapter{Format: AgentClaude},
		Detect:   []DetectRule{{Paths: []string{".acme"}, Weight: 10}},
	}
	h, err := spec.Harness()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAdapters([]*Harness{h}); err != nil {
		t.Fatal(err)
	}
	target := h.TargetPath("project", t.TempDir())
	if target.HooksJSON != "" {
		t.Fatalf("unexpected hooks_json path: %s", target.HooksJSON)
	}
	file, key := h.Hooks.Target(target)
	if file != target.Settings || key != "hooks" {
		t.Fatalf("hook target: %s %s", file, key)
	}
}

func TestAdapterSpecRejectsUnsafeMCPCandidate(t *testing.T) {
	spec := AdapterSpec{
		ID: "acme", Display: "Acme",
		Global:  Paths{Instr: "AGENTS.md", MCP: "mcp.json"},
		Project: Paths{Instr: "AGENTS.md", MCP: ".acme/mcp.json"},
		Bundle:  Paths{Instr: "AGENTS.md", MCP: ".mcp.json"},
		MCP:     &MCPSpec{Format: MCPClaudeJSON, Configs: []string{".mcp.json"}, ProjectCandidates: []string{"../escape.json"}},
	}
	if _, err := spec.Harness(); err == nil {
		t.Fatal("expected unsafe MCP candidate to be rejected")
	}
}

func TestConfigPathIsBounded(t *testing.T) {
	project := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	target := MustHarness("claude").TargetPath("project", project)
	if !configPathAllowed(filepath.Join(project, ".mcp.json"), target) {
		t.Fatal("in-project config must be allowed")
	}
	if configPathAllowed("/etc/passwd", target) {
		t.Fatal("config outside the project must be rejected")
	}
	global := MustHarness("claude").TargetPath("global", "")
	if configPathAllowed("/etc/passwd", global) {
		t.Fatal("global config outside home must be rejected")
	}
}
