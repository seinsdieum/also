package also

func init() { registerAdapter(qwenAdapter()) }

func qwenAdapter() *Harness {
	events := append([]string{}, claudeHookEvents...)
	events = append(events,
		"BeforeTool", "AfterTool", "BeforeAgent", "AfterAgent",
		"BeforeModel", "AfterModel", "BeforeToolSelection", "PreCompress")
	return &Harness{
		ID: "qwen", Display: "Qwen Code", Aliases: []string{"qwen-code", "qwen3"},
		GlobalBase: "~/.qwen",
		Project: Paths{
			Instr: "QWEN.md", Skills: ".qwen/skills", Commands: ".qwen/commands",
			Agents: ".qwen/agents", Hooks: ".qwen/hooks", Settings: ".qwen/settings.json",
			MCP: ".qwen/settings.json",
		},
		Bundle: Paths{
			Instr: "QWEN.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: "settings.json",
		},
		Roots: []string{"", ".qwen", ".agents"},
		Hooks: HookAdapter{
			Configs: []string{"settings.json"},
			Events:  events,
			Target:  func(t Target) (string, string) { return t.Settings, "hooks" },
		},
		MCP: MCPAdapter{Format: MCPQwenJSON, Configs: []string{".mcp.json", "settings.json"}},
		Settings: SettingsAdapter{
			Keys: []string{"permissions"}, Configs: []string{"settings.json"},
			Target: func(t Target) string { return t.Settings },
			Translate: func(settings map[string]any, target *Harness) (map[string]any, []string) {
				return translateClaudeLikeSettings(settings, target, false)
			},
		},
		Commands:  CommandAdapter{Format: CommandQwen},
		Agents:    AgentAdapter{Format: AgentClaude},
		ExtraDirs: []string{"status_lines", "extensions", "themes"},
		ExtraBase: ".qwen",
		Detect: []DetectRule{
			{Paths: []string{"QWEN.md"}, Weight: 10},
			{Paths: []string{".qwen"}, Weight: 6},
			{Paths: []string{".agents"}, Weight: 1},
		},
		InstallHints: []InstallHint{{
			WhenMCP: true,
			Text:    "Qwen Code requires approval for project MCP servers; use /mcp or qwen mcp approve",
		}},
	}
}
