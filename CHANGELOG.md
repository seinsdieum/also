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
- `audit` reports stale harness files and stale config for a harness that is no
  longer installed.
