# Changelog

## 1.1.0 (2026-11-08)

This release turns `also` from an installer into a control plane: it can now
prove what is installed, pin it, gate it and report what changed.

### Added

- `also audit` inventories packages, namespace, content version, full digest,
  harness, source harness, parts, files, managed blocks, config entries and the
  executable surface (MCP servers, hooks, plugins and commands). `--json`,
  `--ci`, `--strict` and `--sbom` are supported.
- `also drift` reports only the files, blocks and config entries that differ
  from the recorded state.
- `also.lock` records the resolved version, digest, namespace, harness, parts
  and original constraint. `also install --frozen` replays it exactly;
  `--update` and `--no-lock` control the file per command.
- Semantic-version aliases over content versions: `also push --semver`,
  `also push --bump major|minor|patch`, `also tag`, and range installs such as
  `package@^1.4`.
- `also.policy.yml` with namespace and package allow/deny lists, executable-kind
  gates, a package size limit and literal-secret blocking. It is enforced before
  install and upgrade writes anything, and re-checked by `also audit --ci`.
- `also search` reads package names and descriptions from every connected
  namespace.
- Installed state records the full digest and executable components; older state
  remains valid and is enriched on the next install.
- `also update` verifies the GitHub build attestation when `gh` is installed and
  authenticated, and reports whether verification ran.
- The release workflow publishes `also.spdx.json` next to the binaries.
- CycloneDX 1.5 SBOM output through `also audit --sbom`.
- Documentation for the lockfile, policy, audit report and semantic aliases.

### Changed

- `also upgrade` respects the constraint recorded in `also.lock` and refreshes
  the lock after a successful upgrade.
- `also uninstall` removes the corresponding lock entry.
- Package size and secret limits now apply to plain-text files as well as JSON,
  YAML and TOML.
- Custom parser fuzz targets cover JSONC edits, TOML blocks, command conversion
  and agent frontmatter.

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

- Instruction blocks are keyed by harness, so one package can be installed for
  OpenCode and Codex in the same directory without colliding.
- `also push` respects `.gitignore`, lists its file manifest with `--dry-run`,
  and requires `--all-files` for files outside the harness layout. Credential
  scanning now covers plain text files too.
- `also uninstall` leaves files, config entries and instruction blocks shared
  with another install in place.
- Backup failures now abort the transaction instead of being ignored; the
  process lock cannot be stolen by a concurrent stale-lock reclaim.
- Remote packages are read with the same size limits as pushed packages.
- Legacy global installs can be listed and removed with `--legacy-global`.
- `also update` tells users on a build newer than the published release to
  reinstall with `install.sh`.
- `also list`/`also verify` batch version reads and run integrity checks with
  bounded parallelism; the update notice no longer blocks non-interactive runs.
- The release workflow runs tests before publishing, and CI exercises
  `install.ps1` on Windows.
- Deleted the unused global install paths, duplicate helpers and the stale
  roadmap; archived the pre-1.0 e2e report under `docs/history/`.
- `also upgrade`, `also install` and `also uninstall` resolve the initialized
  project root when run from a subdirectory.
- Empty upgrade/reshape plans keep the previous install.
- Modified Codex TOML blocks are preserved unless `--force` is used.
- Edited or malformed package tags are reported instead of silently loaded.
- `--offline` refuses a missing cache instead of downloading.
