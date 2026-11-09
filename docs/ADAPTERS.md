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
| `Project`, `Bundle`, `Roots` | Native project layout and bundle roots. |
| `GlobalBase` | Detection hint for `also setup` only. |
| `Hooks` | Source config files, supported events, target file/root key. |
| `MCP` | Source config files, native encoding, optional destination resolver. |
| `Settings` | Mergeable keys, source configs, target file. |
| `Commands`, `Agents`, `Plugins` | Conversion dialect. Empty means unsupported. |
| `ExtraDirs`, `ExtraBase` | Opaque same-harness directories (status lines, themes, ...). |
| `Detect` | Weighted detection rules. |
| `InstallHints` | Install warnings (approval, trust, reload). |

Reusable native encodings live in `MCPFormat`: `claude-json`, `qwen-json`,
`opencode-json`, `codex-toml`. Command dialects are `claude`/`qwen`; agent
dialects are `claude`/`opencode`. A new harness usually reuses one of these.

Agent conversion never carries an incompatible model id across harnesses. An
OpenCode `provider/model` id is dropped for Claude and a Claude alias such as
`sonnet` is dropped for OpenCode; both emit a warning and the harness uses its
own default. For agent frontmatter, OpenCode `permission` deny and ask rules
become a Claude tools allowlist removal: neither has a Claude equivalent, and
dropping an `ask` rule would silently turn it into `allow`. The V2
`permissions` array is handled the same way, including `action: subagent`; a
resource-scoped rule removes the whole tool with a warning.
`permissionMode: plan` becomes disabled edit tools in OpenCode.
`.also-layers/<harness>/` inside the package is an authoritative native override
and its config is merged with the main package.

## Cross-harness settings

The target adapter owns what a cross-harness install can express:
`Settings.Translate` converts source settings into the target dialect, and
`Settings.Reconcile` filters the result against the target's actual config.
Same-harness installs do not call either; they merge `Settings.Keys` as before.

Permissions translate between Claude Code, OpenCode and Qwen Code:

- Source shapes: Claude and Qwen `permissions` with `allow`/`ask`/`deny`
  string arrays; OpenCode v2 `permissions` entries with `action`, `resource`
  and `effect`; OpenCode v1 `permission`, either one global effect string or a
  per-action map.
- Target shapes: Claude and Qwen write `permissions` arrays. OpenCode writes
  the v1 `permission` map with `bash`, `task`, `edit`, `read`, `grep`, `glob`,
  `webfetch`, `websearch`, `list`, `todowrite` and `*`; v2 `shell` and
  `subagent` become `bash` and `task`.
- Fail closed: a scoped deny or ask is broadened to the whole tool with a
  warning; a scoped allow is dropped with a warning; an unknown rule is dropped
  with a warning. deny outranks ask, and ask outranks allow. A translated rule
  never replaces a target deny or ask with allow, and `--force` does not weaken
  a scoped target map or a stronger existing effect.
- OpenCode v2 target: a non-empty target `permissions` array is left untouched;
  translated rules are not applied and stay in the package with a warning. A
  target scoped `permission.<action>` map is kept and warned about. A target
  global string becomes a map only when a translated specific rule is stronger.
- Codex has no mergeable per-tool settings or permission model: it returns no
  settings and warns that they remain in the package.

`theme` transfers to Claude Code and OpenCode with a warning that the name may
not exist there. `model`, `statusLine`, `small_model`, `default_agent`,
`keybinds` and `experimental` have no cross-harness equivalent: when the source
adapter declares them, they remain in the package with one warning per key. A
same-harness Claude install still does not install a package `theme`, because
translation runs only when source and target differ.

An upgrade keeps previously installed deny/ask and scoped restrictions and
drops stale allow grants. A translated permission map that replaced a global
string is owned as one whole value, so uninstall restores the original string.
An adapter with no `Settings.Translate` keeps all settings in the package with
the generic `settings <keys> are preserved in the package but not installed
cross-harness` warning.

`Settings.Keys` is an allowlist of reversible top-level config keys, not a
whole-file install. For OpenCode it covers `default_agent`, `experimental`,
`theme`, `model`, `small_model`, `keybinds`, `permission` and the V2
`permissions`; MCP and hooks are merged separately. `Settings.CrossKeys`
(for example Claude's `theme`) are extracted for cross-harness translation only
and are not installed on a same-harness install.

An OpenCode package whose agents already use the native V2 `permissions` array
is copied verbatim into agent frontmatter on an OpenCode install, so
`action: subagent` and `external_directory` work as written. Only cross-harness
agent conversion rewrites that array. OpenCode v2 accepts V1 `permission`,
`tools` and `temperature` through its compatibility layer; `temperature` is
stored but not sent by v2. Each settings key is written and reversed
independently, so a package can carry the intended agent and model defaults
without replacing the whole file; the one exception is the whole-map permission
owner described above. Add a key to the adapter when the value is part of the
infrastructure contract.

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

A harness that reuses an existing format can be added without recompiling:

```sh
also add harness ./acme.json
also add harness https://example.com/pi-adapter.json
also list --harnesses
also remove harness acme
```

The file is validated first, then saved under `~/.also/adapters/<id>.json`
(or `ALSO_ADAPTERS=<dir>`). A community spec cannot shadow a built-in id or
claim an alias that is already taken. `global_base` is only a detection hint
and must be a directory below `$HOME`; all install paths are relative to the
project root, so a downloaded spec cannot write to unrelated files.

Downloaded specs can be pinned with `also add harness <url> --sha256 <hash>`.
The hash covers the exact file bytes; an unpinned URL still works but prints a
warning. Adapters cannot execute code, so the remaining risk is choosing where
files are written, which the path bounds above are meant to contain. See
[SECURITY.md](../SECURITY.md) for the full trust model.

The JSON shape is:

```json
{
  "id": "acme",
  "display": "Acme Agent",
  "aliases": ["ac"],
  "global_base": "~/.acme",
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
  (`configPathAllowed`);
- cross-harness settings are written only through the target adapter's
  `Translate`/`Reconcile`; everything else stays in the package with a warning,
  and a target's V2 permission array or scoped permission map is never
  overwritten;
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

