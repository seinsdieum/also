# Changelog

## 1.0.0 (unreleased)

### Added

- Remote-only namespaces: a namespace is a name plus a git URL; the local
  clone is an internal cache.
- `also connect <name> <git-url>` and `also setup`, interactive or `--yes`.
- `install.sh`, `install.ps1` and `go install github.com/seinsdieum/also@latest`.
- `adopt` publishes straight to the namespace without a local source directory.
- `also doctor`, `also verify` and `also repair`.
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
- Module path is `github.com/seinsdieum/also`; the project is MIT licensed.

### Fixed

- Empty upgrade/reshape plans keep the previous install.
- Modified Codex TOML blocks are preserved unless `--force` is used.
- Edited or malformed package tags are reported instead of silently loaded.
- `--offline` refuses a missing cache instead of downloading.
