# Changelog

## 0.1.0 (2026-11-08)

This is the first public version. The project renumbered to `0.1.0` before
committing to a stable CLI contract, so the command surface and JSON shapes can
still change in a minor release until `1.0.0`.

### The CLI surface

Thirteen visible commands. Four are the core workflow:

- `also install` — materialize a package into a project for one harness.
- `also push` — publish the initialized infrastructure as a content-addressed
  version.
- `also audit` — show what is installed, where it came from and what changed.
- `also uninstall` — remove a package and reverse its files and config edits.

The rest are setup, inspection and maintenance: `setup`, `init`, `connect`,
`adopt`, `upgrade`, `tag`, `list`, `show`, `doctor`.

Read-only modes are flags on `audit` and `list`, not separate commands:
`audit --drift`, `audit --verify`, `audit --policy`, `list <query>`,
`list --tree`, `list --harnesses`. `connect <name>` chooses the default
namespace. `doctor` prunes stale install records automatically and `doctor
--fix` re-clones dirty namespace caches.

Advanced or plumbing commands remain callable but are hidden from `--help`:
`version`, `config`, `exclude`, `include`, `policy keygen|trust|sign|verify`,
`reshape`, `add harness`, `remove harness`.

### What the tool does

- Content-addressed packages in a git namespace, with immutable semantic
  aliases and a committed `also.lock`.
- Reversible installs: files, managed instruction blocks and native config
  edits are recorded and restored in any uninstall order.
- Artifact-free product repositories: `install`, `upgrade` and `reshape`
  maintain a managed `.gitignore` block, and `audit --ci` fails on tracked
  infrastructure.
- Signed policy governance: `also.policy.yml`, a detached Ed25519 signature and
  a trust anchor outside the policy.
- Cross-harness translation for Claude Code, OpenCode, Codex and Qwen, with
  warnings instead of silently writing a component the target ignores.
- Lock-only CI replay: `install --frozen` uses the remote recorded in the lock
  and preflights every entry before writing.

### Fixed before the first release

- Config bases and created-file provenance live in `~/.also/config-origins`, so
  a same-machine state loss still uninstalls reversibly; a manual edit after
  state loss requires `--force` instead of being overwritten.
- `reshape` is project-scoped, updates the lock and the managed ignore block,
  preflights every package and rolls earlier packages back on a later failure.
- The lock is keyed by namespace, package and harness; two namespaces can carry
  the same package name.
- `upgrade` stays inside the installed major unless `--major` is passed, and a
  downgrade is refused.
- Shared identical config values and files are tracked as ownership, so the
  surviving package keeps its value and the last uninstall restores the user's
  base.
- `audit` reports stale harness files and stale executable config, including
  `opencode.jsonc`, nested project configs and config for an installed harness
  that no install owns. A user-owned config with only cosmetic settings is not
  flagged.
- Every `also.policy.yml` from the project to the git root is enforced. A
  nested policy can add restrictions but cannot disable a root deny, executable
  gate or signature requirement.
- `reshape` enforces the policy before writing. `upgrade` preflights every
  package before applying any, and a failed upgrade leaves the lock and
  `.gitignore` unchanged.
- `audit --verify` derives namespace remotes from the lock and state, so it
  works on a lock-only CI checkout.
- The process lock waits up to 30 seconds for another command and reclaims a
  crashed lock after 10 minutes instead of blocking for 30.
- `push --allow-empty` works on a truly empty source, and empty collections in
  JSON output are `[]`, not `null`.
- A multi-namespace upgrade refreshes only the namespace/harness/scope that
  actually advanced, so a sibling install keeps its own constraint.
- `audit` checks every MCP spelling; an empty `mcp.servers` no longer hides a
  non-empty `mcpServers` or `mcp_servers` block. Generic config names such as
  `settings.json` are flagged only under a harness directory, so a product's
  own `web/settings.json` is not mistaken for infrastructure.
- The policy chain stops at a real git root, not at a directory that merely
  contains an empty `.git`. `audit --json` reports the policy file that was
  actually loaded.
