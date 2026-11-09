package also

func init() { registerAdapter(claudeAdapter()) }

var claudeHookEvents = []string{
	"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest",
	"UserPromptSubmit", "Notification", "Stop", "SubagentStop", "SubagentStart",
	"PreCompact", "SessionStart", "SessionEnd", "Setup",
}

func claudeAdapter() *Harness {
	return &Harness{
		ID: "claude", Display: "Claude Code", Aliases: []string{"claude-code", "cc"},
		GlobalBase: "~/.claude",
		Project: Paths{
			Instr: "CLAUDE.md", Skills: ".claude/skills", Commands: ".claude/commands",
			Agents: ".claude/agents", Hooks: ".claude/hooks", Settings: ".claude/settings.json",
			MCP: ".mcp.json",
		},
		Bundle: Paths{
			Instr: "CLAUDE.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: ".mcp.json",
		},
		Roots: []string{"", ".claude"},
		Hooks: HookAdapter{
			Configs: []string{"settings.json"},
			Events:  claudeHookEvents,
			Target:  func(t Target) (string, string) { return t.Settings, "hooks" },
		},
		MCP: MCPAdapter{Format: MCPClaudeJSON, Configs: []string{".mcp.json", "settings.json"}},
		Settings: SettingsAdapter{
			Keys: []string{"permissions", "statusLine"}, CrossKeys: []string{"theme"},
			Configs: []string{"settings.json"},
			Target:  func(t Target) string { return t.Settings },
			Translate: func(settings map[string]any, target *Harness) (map[string]any, []string) {
				return translateClaudeLikeSettings(settings, target, true)
			},
			Reconcile: reconcileClaudeSettings,
		},
		Commands:  CommandAdapter{Format: CommandClaude},
		Agents:    AgentAdapter{Format: AgentClaude},
		ExtraDirs: []string{"status_lines", "output-styles", "themes"},
		ExtraBase: ".claude",
		Detect: []DetectRule{
			{Paths: []string{"CLAUDE.md"}, Weight: 10},
			{Paths: []string{".claude"}, Weight: 6},
		},
		InstallHints: []InstallHint{{
			WhenMCP: true,
			Text:    "Claude Code requires explicit approval for project .mcp.json servers; run claude once and approve them",
		}},
	}
}
