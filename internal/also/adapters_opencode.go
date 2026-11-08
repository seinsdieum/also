package also

import (
	"os"
	"path/filepath"
)

func init() { registerAdapter(opencodeAdapter()) }

func opencodeAdapter() *Harness {
	return &Harness{
		ID: "opencode", Display: "OpenCode", Aliases: []string{"oc", "open-code"},
		GlobalBase: "~/.config/opencode",
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".opencode/skills", Commands: ".opencode/commands",
			Agents: ".opencode/agents", Plugins: ".opencode/plugins", Hooks: ".opencode/hooks",
			MCP: "opencode.json",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Plugins: "plugins", Hooks: "hooks", MCP: "opencode.json",
		},
		Roots: []string{"", ".opencode"},
		MCP: MCPAdapter{
			Format:  MCPOpenCodeJSON,
			Configs: []string{"opencode.json", "opencode.jsonc"},
			Resolve: pickOpenCodeConfig,
		},
		Settings: SettingsAdapter{
			Keys:    []string{"default_agent", "keybinds", "model", "permission", "permissions", "small_model", "theme"},
			Configs: []string{"opencode.jsonc", "opencode.json"},
			Target:  func(t Target) string { return t.MCP },
		},
		Commands:  CommandAdapter{Format: CommandClaude},
		Agents:    AgentAdapter{Format: AgentOpenCode},
		Plugins:   PluginAdapter{Format: PluginOpenCode},
		ExtraDirs: []string{"themes"},
		ExtraBase: ".opencode",
		Detect: []DetectRule{
			{Paths: []string{"opencode.json"}, Weight: 10},
			{Paths: []string{"opencode.jsonc"}, Weight: 10},
			{Paths: []string{".opencode"}, Weight: 6},
			{Paths: []string{".agents"}, Weight: 1},
		},
		InstallHints: []InstallHint{{
			WhenMCP: true, WhenSettings: true,
			Text: "OpenCode watches project config, but a long-running service may need: opencode reload",
		}},
	}
}

func pickOpenCodeConfig(project string) string {
	candidates := []string{
		filepath.Join(project, ".opencode", "opencode.jsonc"),
		filepath.Join(project, ".opencode", "opencode.json"),
		filepath.Join(project, "opencode.jsonc"),
		filepath.Join(project, "opencode.json"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
			return c
		}
	}
	return filepath.Join(project, "opencode.json")
}
