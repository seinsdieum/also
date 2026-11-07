# Changelog

## 1.0.0 (2026-11-07)

The first stable release. The initialized directory is the unit of work:
`also init` marks it, `also push` publishes it, and `also install` and
`also upgrade` act on it without a project/global split.

### Added

- `also init <name> [for <harness>]` writes `also.json` with the package name
  and source harness.
- `also push` publishes the nearest initialized directory with no path, name
  or harness argument. A directory without `also.json` is refused.
- `also adopt for <harness>` captures the initialized directory.
- `also upgrade` upgrades the packages installed in the current directory and
  uses the initialized name when one exists.
- Remote-only namespaces: a namespace is a name plus a git URL; the local clone
  is an internal cache.
- `also connect <name> <git-url>` and `also setup`, interactive or `--yes`.
- `install.sh`, `install.ps1` and `go install github.com/seinsdieum/also@latest`.
  The installers verify the release checksum and launch `also setup` directly.
- Content-addressed package versions, immutable version tags and pinned
  `package@version` installs.
- Built-in adapters for Claude Code, Codex CLI, OpenCode and Qwen Code, plus
  declarative community adapters with `--sha256` pinning.
- Reversible installs, partial installs and preserved native layers for
  round-trip translation.
- `also doctor`, `also verify` and `also repair`.
- `also update`, a once-per-day release check and a single “New version
  available” notice on standard error.
- JSON output for install, push, adopt, upgrade, reshape, uninstall, setup and
  version, with documented exit codes.
- In-repo 4×4 harness matrix and a real-CLI smoke from `make smoke`.
- Format contracts and versioning policy in `docs/CONTRACTS.md`.

### Changed

- Package references use `from <namespace>` instead of `namespace@package`.
- Installs always target the current directory or the initialized parent.
  The `--scope` flag and `default_scope` config key are gone.
- `--project` is now `--dir`.
- `also update` only updates the binary; package upgrades use `also upgrade`.
- The license and NOTICE name Aliaksandr Sialitski.

### Fixed

- `also upgrade`, `also install` and `also uninstall` resolve the initialized
  project root when run from a subdirectory.
- Empty upgrade/reshape plans keep the previous install.
- Modified Codex TOML blocks are preserved unless `--force` is used.
- Edited or malformed package tags are reported instead of silently loaded.
- `--offline` refuses a missing cache instead of downloading.
