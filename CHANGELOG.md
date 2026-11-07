# Changelog

## 2.0.0 (2026-11-07)

This release makes the working directory the unit of work. It removes the
project/global split and the old `namespace@package` spelling, so it is a
major release under the policy in `docs/CONTRACTS.md`.

### Added

- `also init <name> [for <harness>]` writes `also.json` into the current
  directory and records the package name and source harness.
- `also push` publishes the nearest initialized directory with no path, name or
  harness argument. A directory without `also.json` is refused.
- `also adopt` captures the initialized directory, so `also adopt for claude`
  replaces `also adopt . for claude`.
- `also upgrade` upgrades the package installed in the current directory and
  uses the initialized name when one exists.

### Changed

- Removed the project/global scope split. Installs always target the current
  directory (or the initialized parent), and the `--scope` flag and
  `default_scope` config key are gone.
- Package references no longer use `namespace@package`. Choose a namespace with
  `from <namespace>`, for example `also install code-review for opencode from
  personal`.
- `also upgrade` is no longer an alias of `also update`; `also update` only
  updates the binary.
- The README and the hero illustration now lead with the CLI: adaptive light
  and dark themes, system fonts, no counters, and the harness-agnostic and
  remote-namespace ideas in plain language.
- Removed the second illustration.
- The license and NOTICE name Aliaksandr Sialitski.

### Fixed

- `also upgrade`, `also install` and `also uninstall` resolve the initialized
  project root when run from a subdirectory.

## 1.0.2 (2026-11-06)

- Resolve the latest release through the GitHub redirect instead of the
  unauthenticated API, which avoids its hourly request limit.
- Support `ALSO_RELEASE_LATEST_URL` and `ALSO_RELEASE_DOWNLOAD_BASE` for mirrors
  and for testing the update path against a local release server.

## 1.0.1 (2026-11-06)

- Retry transient GitHub connection failures during release checks and
  downloads, so `also update` survives a closed connection.

## 1.0.0 (2026-11-06)

### Added

- Remote-only namespaces: a namespace is a name plus a git URL; the local
  clone is an internal cache.
- `also connect <name> <git-url>` and `also setup`, interactive or `--yes`.
- `install.sh`, `install.ps1` and `go install github.com/seinsdieum/also@latest`.
  The installers verify the release checksum and launch `also setup` directly.
- `adopt` publishes straight to the namespace without a local source directory.
- `also doctor`, `also verify` and `also repair`.
- `also update`, a once-per-day release check and a single “New version
  available” notice on standard error.
- A cobalt-blue installer and diagnostic design with one shared accent colour.
- JSON output for install/push/adopt/upgrade/reshape/uninstall/setup,
  `also version --json`, and documented exit codes.
- In-repo 4×4 harness matrix and a real-CLI smoke from `make smoke`.
- Adapter pinning with `--sha256`, adapter schema `also.adapter/v1`, and
  credential scanning for JSON/YAML/TOML settings, MCP args and URLs.
- Format contracts and versioning policy in `docs/CONTRACTS.md`.

### Changed

- Config schema is `also.config/v2`; `also.config/v1` migrates automatically.
- `--namespace` is the documented flag; `--set` and `also set` are hidden
  aliases for older scripts.
- Version tags are validated against their manifest digest; mismatched or
  orphan tags are rejected.
- Install slots include the target harness, so one package can live in several
  harnesses in the same project.
- Module path is `github.com/seinsdieum/also`; the project is licensed under the
  Apache License 2.0 with a NOTICE file that preserves attribution.

- The README is written in full English and uses a six-part structure:
  problem, quick start, overview, contributing, credits and license.
- Repository illustrations use transparent backgrounds and one cobalt-blue
  visual language.

### Fixed

- Empty upgrade/reshape plans keep the previous install.
- Modified Codex TOML blocks are preserved unless `--force` is used.
- Edited or malformed package tags are reported instead of silently loaded.
- `--offline` refuses a missing cache instead of downloading.
