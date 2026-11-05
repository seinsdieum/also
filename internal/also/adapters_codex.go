package also

func init() { registerAdapter(codexAdapter()) }

var codexHookEvents = []string{
	"PreToolUse", "PermissionRequest", "PostToolUse", "PreCompact", "PostCompact",
	"UserPromptSubmit", "SubagentStop", "SubagentStart", "Stop", "SessionStart",
	"SessionEnd", "PreModelRequest", "PostModelRequest", "Interrupt",
}

func codexAdapter() *Harness {
	return &Harness{
		ID: "codex", Display: "Codex CLI", Aliases: []string{"codex-cli"},
		GlobalBase: "~/.codex",
		Global: Paths{
			Instr: "AGENTS.md", Skills: "~/.agents/skills", Hooks: "hooks",
			HooksJSON: "hooks.json", MCP: "config.toml",
		},
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".agents/skills", Hooks: ".codex/hooks",
			HooksJSON: ".codex/hooks.json", MCP: ".codex/config.toml",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Hooks: "hooks",
			HooksJSON: "hooks.json", MCP: "config.toml",
		},
		Roots: []string{"", ".agents", ".codex"},
		Hooks: HookAdapter{
			Configs: []string{"hooks.json", "config.toml"},
			Events:  codexHookEvents,
			Target:  func(t Target) (string, string) { return t.HooksJSON, "hooks" },
		},
		MCP:       MCPAdapter{Format: MCPCodexTOML, Configs: []string{"config.toml"}},
		ExtraDirs: []string{"themes"},
		ExtraBase: ".codex",
		Detect: []DetectRule{
			{Paths: []string{".codex", "hooks.json"}, Weight: 8},
			{Paths: []string{"config.toml"}, Weight: 6},
			{Paths: []string{".agents"}, Weight: 1},
		},
		InstallHints: []InstallHint{{
			Text: "Codex loads project .codex/config.toml and hooks.json only after the project is trusted; trust the directory first",
		}},
	}
}
