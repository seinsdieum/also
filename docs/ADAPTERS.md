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

If the harness has a genuinely new encoding, register a `MCPCodec` (or set a
custom `Hooks.Extract`/`Settings.Extract`) in an init function and reference it
from the adapter. Everything else stays in the adapter.

## Format engines

MCP encodings are registered codecs (`MCPCodec`): extraction, rendering and the
reversible config edit live together. New encodings call `RegisterMCPCodec` in
an init function and are selected by `MCP.Format`; the installer has no switch
over harnesses. Built-ins live in `mcp_formats.go`.

Command and agent dialects are registries too: `RegisterCommandFormat` /
`RegisterCommandConverter` and `RegisterAgentFormat` / `RegisterAgentConverter`.
A new dialect registers its pair once; `ConvertCommand`/`ConvertAgent` do not
grow another switch.

Hooks and settings use a safe default (JSONC/TOML key extraction). An adapter
with a different format sets `Hooks.Extract` or `Settings.Extract` and owns the
parsing itself. Rendering still goes through the adapter's `Target` function.

## Custom component kinds

Beyond instructions/skills/commands/agents/plugins/hooks, an adapter can declare
`Components` such as workflows or prompts:

```go
Components: []Component{{
    Kind: "workflows", BundleDir: "workflows",
    ProjectDir: ".acme/workflows", GlobalDir: "workflows",
    Extensions: []string{".md"},
}},
```

The system validates the paths, classifies the bundle files, renders them into
the target directory and adopts them back. `Opaque` restricts a kind to the same
harness; `Format` restricts it to matching formats; `Convert` is optional.

## External declarative adapters

A harness that reuses an existing format can be added without recompiling by
putting JSON under `~/.also/adapters/` (or `ALSO_ADAPTERS=<dir>`):

```json
{
  "id": "acme",
  "display": "Acme Agent",
  "aliases": ["ac"],
  "global_base": "~/.acme",
  "global":  {"instr": "AGENTS.md", "skills": "skills", "commands": "commands", "agents": "agents", "settings": "settings.json", "mcp": "mcp.json"},
  "project": {"instr": "AGENTS.md", "skills": ".acme/skills", "commands": ".acme/commands", "agents": ".acme/agents", "settings": ".acme/settings.json", "mcp": ".acme/mcp.json"},
  "bundle":  {"instr": "AGENTS.md", "skills": "skills", "commands": "commands", "agents": "agents", "settings": "settings.json", "mcp": ".mcp.json"},
  "roots": ["", ".acme"],
  "hooks":    {"configs": ["settings.json"], "events": ["PreToolUse"], "target": "settings"},
  "mcp":      {"format": "claude-json", "configs": [".mcp.json"]},
  "settings": {"keys": ["permissions"], "configs": ["settings.json"], "target": "settings"},
  "commands": {"format": "claude"},
  "agents":   {"format": "claude"},
  "detect":   [{"paths": [".acme"], "weight": 10}]
}
```

External specs cannot execute code. They can only select registered formats and
declare paths; the same registry validation and runtime path bounds apply. An
invalid or duplicate spec is skipped with a warning, not trusted.

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

Adapters may be compiled in or loaded as declarative JSON specs. There is no
dynamic code loading: a spec can only select a registered format/codec, never
execute arbitrary code. This keeps the security boundary auditable: adapters can
choose formats and paths, but the system validates and bounds every destination
at install time.

## Known next refactors

1. **Standard component fields.** Instructions/skills/commands/agents/plugins/
   hooks are still named fields on `Paths`; custom kinds already go through
   `Components`. Moving the standard kinds into the same collection model would
   remove the remaining field plumbing without changing behaviour.
2. **Non-JSON settings formats.** `Settings.Extract` covers reading, but the
   reversible edit path is JSON/TOML. A YAML/INI settings config would need a
   matching `SettingsCodec` writer.
3. **Version store object reuse.** Old versions are reconstructed from git
   commits on demand. Git already deduplicates objects; a local cache could make
   repeated installs of old versions cheaper.

