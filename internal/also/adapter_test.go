package also

import (
	"path/filepath"
	"testing"
)

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
