# Adapters

`also` separates orchestration from harness knowledge.

- **System** (`install`, `render`, `snapshot`, `state`, `settings`, `mcp`, `hooks`):
  reads a package, verifies its digest, decides what is portable, writes files and
  reversible config edits, and enforces safety boundaries.
- **Adapter** (`adapters_<id>.go`): describes one harness and owns every
  harness-specific decision: native paths, capabilities, config formats,
  conversion dialects, detection rules and install hints.

A new harness is a new adapter file plus `init()` registration. Generic code does
not need an `if harness == "..."` branch.

## Contract

`Harness` in `internal/also/adapter.go` is the adapter contract:

| Field | Meaning |
|---|---|
| `ID`, `Display`, `Aliases` | Identity and CLI aliases. |
| `Global`, `Project`, `Bundle`, `Roots` | Native layout and bundle roots. |
| `Hooks` | Source config files, supported events, target file/root key. |
| `MCP` | Source config files, native encoding, optional destination resolver. |
| `Settings` | Mergeable keys, source configs, target file. |
| `Commands`, `Agents`, `Plugins` | Conversion dialect. Empty means unsupported. |
| `ExtraDirs`, `ExtraBase` | Opaque same-harness directories (status lines, themes, ...). |
| `Detect` | Weighted detection rules. |
| `InstallHints` | Project-scope warnings (approval, trust, reload). |

Reusable native encodings live in `MCPFormat`: `claude-json`, `qwen-json`,
`opencode-json`, `codex-toml`. Command dialects are `claude`/`qwen`; agent
dialects are `claude`/`opencode`. A new harness usually reuses one of these.

## Adding a harness

1. Create `internal/also/adapters_<id>.go`.
2. Fill the layout and capabilities. Reuse formats where the ecosystem matches.
3. Register it:

```go
func init() { registerAdapter(myHarnessAdapter()) }
```

4. Add a detection rule and, if needed, an `InstallHint`.
5. Run tests. `TestAdaptersAreValid` validates every registered adapter.

Example skeleton:

```go
func init() { registerAdapter(myAdapter()) }

func myAdapter() *Harness {
	return &Harness{
		ID: "myharness", Display: "My Harness",
		GlobalBase: "~/.myharness",
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".myharness/skills",
			Commands: ".myharness/commands", Agents: ".myharness/agents",
			Settings: ".myharness/settings.json", MCP: ".myharness/mcp.json",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands",
			Agents: "agents", Settings: "settings.json", MCP: ".mcp.json",
		},
		Roots: []string{"", ".myharness"},
		Hooks: HookAdapter{
			Configs: []string{"settings.json"},
			Events:  claudeHookEvents,
			Target:  func(t Target) (string, string) { return t.Settings, "hooks" },
		},
		MCP:      MCPAdapter{Format: MCPClaudeJSON, Configs: []string{".mcp.json"}},
		Settings: SettingsAdapter{Keys: []string{"permissions"}, Configs: []string{"settings.json"}, Target: func(t Target) string { return t.Settings }},
		Commands: CommandAdapter{Format: "claude"},
		Agents:   AgentAdapter{Format: "claude"},
		Detect:   []DetectRule{{Paths: []string{".myharness"}, Weight: 10}},
	}
}
```

If the harness has a genuinely new encoding, add a format engine next to the
existing ones and a new `MCPFormat`/dialect constant. This is the only part that
requires touching generic code; everything else stays in the adapter.

## Validation

`registerAdapter` runs validation during `init`, so an invalid built-in adapter
fails fast. `ValidateAdapters` checks a whole registry (used by tests). Rules:

- IDs and aliases are safe, unique and lowercase.
- Every path is relative and cannot escape its root; project paths may not be
  absolute. Bundle paths and config names are validated.
- MCP/hook/settings formats must be known; capability formats require native
  directories; `codex-toml` requires TOML configs.
- Hook support requires events and a target; settings require source configs.
- Detect weights are positive; extra dirs require an `ExtraBase`.

Runtime safety is enforced independently of adapter declarations:

- rendered files must stay under the target root (`fileAllowedRoot`);
- adapter-chosen MCP/hooks/settings destinations must stay inside the project
  (or under `$HOME` for global installs) (`configPathAllowed`);
- cross-harness permissions are never translated automatically;
- native layers preserve what the target cannot represent;
- hook command bodies are copied verbatim with a warning.

## Operating-system adaptation

OS differences are deliberately **not** adapters. They are system invariants:

- set clones force LF (`core.autocrlf=false`, `core.eol=lf`) and repair old
  CRLF clones, because digests cover worktree bytes;
- package modes come from the git index (`100644`/`100755`), not the worktree,
  because Windows has no executable bit;
- package-relative paths use slash-aware helpers, never `filepath.Dir` on
  slash-separated keys.

An adapter may not loosen these rules. They apply identically to every adapter.

## Limits

Adapters are compiled in; there is no dynamic code loading. A community adapter
is a Go file in this repository, reviewed and tested like any other code. This
keeps the security boundary auditable: adapters can choose formats and paths,
but the system validates and bounds every destination at install time.

## Known next refactors

These are the remaining hardcoded patterns, in priority order:

1. **Component kinds.** `Paths` and `bundleParts` still name instructions,
   skills, commands, agents, plugins, hooks and extras explicitly. A future
   component (for example workflows or prompts) needs a core change. The next
   step is a kind registry where an adapter maps a kind to bundle and target
   paths; `render` and `install` would iterate kinds instead of struct fields.
2. **Format engines.** MCP/hook/settings encodings are enum values with a
   switch. A genuinely new encoding needs a core engine. Moving them behind
   `MCPEncoder`/`HookEncoder` interfaces would let an adapter ship its own
   implementation.
3. **External adapters.** Dynamic loading is intentionally absent. If needed,
   load declarative specs only (no code), validate them with the same rules, and
   require an existing format engine. Never let a spec choose an unbounded path.

