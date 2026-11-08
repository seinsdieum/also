# Changelog

## 1.4.0 (2026-11-08)

This release closes the self-hosting gaps found by running `also` against
itself: artifact-free product repositories, multi-project state, package
ownership and harness migration.

### Added

- `audit --ci` fails on installed infrastructure that is tracked by git:
  `infra-tracked`, `block-tracked` and `config-tracked`.
- `install --except <kind|name>` and `--only`: install everything except
  instructions/settings/MCP, or replace the previous selection. Multiple part
  selectors are accepted in one command.
- Additive partial installs: adding agents keeps existing skills.
- `install --latest` opts out of the default `^major.minor.patch` pin.
- `upgrade --major` is required to cross a major version; the lock constraint
  moves to the new major and a downgrade is refused.
- A plain `also install <package>` installs the whole package again, resetting
  a previous partial selection.
- `reshape --dir <path>` and `--all`; project scope is the default and the lock
  is updated after a reshape.
- `uninstall --namespace <name>` for the same package from two namespaces.
- `list --installs` shows every install with its project; `audit --all` prints
  the project column.
- `push --allow-empty` for an intentional metadata-only package; such a package
  is refused by default.
- `also.policy` now records approval roles and signatures in `also policy`.

### Fixed

- `exclude infra` uses the project layout before the machine default harness,
  so an OpenCode project cannot receive Claude ignore entries.
- Commands resolve the nearest project root (`also.lock`, `also.json`, then the
  git root); running from a subdirectory no longer creates a nested project.
- Config ownership is order-independent: identical values from two packages are
  recorded, a remaining package keeps its value, and the last uninstall
  restores the original user value or removes the key/file.
- The lock is keyed by namespace, package and harness, so two namespaces can
  carry the same package name.
- `install --frozen` uses the remote recorded in the lock and preflights every
  entry before writing; a fresh CI checkout needs no namespace setup, and a
  restored tree with identical files is adopted without `--force`.
- OpenCode/Claude agent conversion drops the wrong model id, maps permission
  deny rules to a Claude tools allowlist, maps `permissionMode: plan`, and warns
  instead of silently writing an invalid model.
- `.also-layers/<harness>` is an authoritative native override; layer config is
  merged with the main package instead of replacing it.
- Uninstall removes an instruction file it created when the last block is gone.
- State and lock reads are refreshed under the transaction lock for uninstall,
  upgrade, reshape, prune and frozen replay.
- `upgrade` and `reshape` on a fresh lock-only machine keep the recorded
  namespace instead of inventing `remote-<hash>`, so no stale managed block or
  duplicate install slot is left behind.
- `reshape` updates the managed infra `.gitignore` block at the git root for
  the new harness, unions every harness installed under that root, and removes
  the old harness's files and config edits before writing the new layout.
- `audit` reports `stale-infra` for tracked files under any registered harness
  directory (including another harness than the one currently installed),
  nested projects, harness-root files and an empty lock after uninstall.
- `install --frozen` preflights destination conflicts, cross-entry config
  conflicts and policy executables before writing the first entry, and rolls
  back entries it applied if a later write fails, restoring any pre-existing
  files it adopted.
- `push` refuses a source instruction file that already contains an also-managed
  block; use `adopt` to capture a live installation.
- `reshape` preflights every package before applying any, and checks every
  project before `--all`; it refuses a symlinked `.gitignore` before mutating.
- Scoped `exclude infra for <harness>` merges with the shared block instead of
  replacing it; scoped `include` removes only entries no remaining harness
  needs, so a shared `AGENTS.md` stays ignored for Codex.
- `reshape` does not fail after mutation in a checkout without `.git`.
- OpenCode wildcard/pattern deny rules remove the affected Claude tool instead
  of silently keeping full access.
- The lock records the installed semantic alias; `audit` rejects a constraint
  that no longer matches the pinned version.
- A config key created by a package keeps an absent base across upgrades, so
  uninstalling does not restore the previous package value.
- Shared identical files/blocks/config entries are `info`, so `audit --strict`
  does not fail legitimate duplicate namespace installs.
- A plain `install` detects the project layout (OpenCode, Claude, ...) before
  falling back to the machine default harness.

### Changed

- `also install` targets the project root, not the literal current directory.
- A fresh install without a version pins the resolved semantic version.
- `also upgrade` stays inside the installed major unless `--major` is passed.
- `audit --ci` now rejects tracked harness files; the artifact-free layout is a
  CI invariant.
- Cross-harness agents with an incompatible model are installed without a model
  and emit a warning rather than passing an invalid id to the harness.

## 1.3.0 (2026-11-08)

This release makes policy governance cryptographic and removes the remaining
local escape hatches.

### Added

- Signed policies: `also.policy.sig` with detached Ed25519 signatures, roles
  and approvals. New commands: `also policy keygen`, `also policy trust`,
  `also policy sign` and `also policy verify --ci`.
- A trust anchor outside the policy: `~/.also/trusted-policy-keys` or
  `ALSO_TRUSTED_POLICY_KEYS`. A policy cannot trust its own key.
- `governance.require_signature`, `required_approvals`, `required_roles` and an
  approver list with names, roles and public keys.
- `audit --ci` fails on an unsigned policy, an invalid or untrusted signature, a
  missing required role and insufficient approvals, in addition to lock and
  drift failures.
- Windows control-plane E2E in CI and a 101-version scale test.

### Changed

- Semantic-version aliases are immutable by default. `also tag --move` is the
  explicit retarget path and uses a force-with-lease push that refuses an unseen
  remote change.
- OpenCode round-trips `theme`, `model`, `small_model` and `keybinds` in
  addition to `default_agent`, `permission` and `permissions`.

### Removed

- `--no-lock` on `install` and `upgrade`. The lockfile is always read and
  written; `--frozen` replays it and `--update` still records the result.

## 1.2.2 (2026-11-08)

### Fixed

- OpenCode's current `permission` key (and the legacy `permissions` key) is
  captured and installed reversibly together with `default_agent`. Unrelated
  `opencode.json` keys remain untouched.

## 1.2.1 (2026-11-08)

### Fixed

- `also uninstall` no longer reads a shifted install record after removing the
  first of several packages. The output and `also.lock` now name and remove the
  package that was actually uninstalled. `FindByID` returns a copy to prevent
  the same class of bug in other callers.
- OpenCode's `default_agent` is captured by `adopt` and installed reversibly
  into `opencode.json`; unrelated keys stay untouched. The adapter allowlist
  is documented in `docs/ADAPTERS.md`.
- `also adopt` accepts `--semver` and `--bump`, so adopting and tagging is one
  publish instead of a second `push` with different content metadata.
- `also list` shows the installation directory, not only the harness.
- `also prune` removes state records whose project directory was deleted
  manually; `doctor` points to it. Existing projects with missing files are
  still left to `audit` and `drift`.
- The control-plane E2E now installs two packages, uninstalls one, and verifies
  that the surviving lock entry is the correct one.

## 1.2.0 (2026-11-08)

This release makes the committed lockfile the root of trust and tightens the
release gate.

### Added

- `also audit --ci` now requires a git-tracked `also.lock` and compares every
  lock entry with the local state. New findings: `lock-missing`,
  `lock-untracked`, `lock-invalid`, `lock-not-installed`,
  `lock-version-mismatch`, `lock-digest-mismatch`,
  `lock-namespace-mismatch`, `lock-digest-missing`, `lock-config-mismatch` and
  `lock-extra`. `~/.also/state.json` is treated as the local cache.
- `make release-check` runs formatting, vet, the full suite, the race detector,
  `go mod verify` and the committed-lock E2E.
- `test/e2e/control-plane.sh` is part of CI: it reproduces a lock, audits it,
  emits an SBOM, and proves that policy denial and drift fail the gate.
- `docs/RELEASING.md` documents the release checklist and version policy.
- Bug and feature issue templates.

### Changed

- Semantic versions now use `github.com/Masterminds/semver/v3`; the custom range
  parser is gone. The public behavior is unchanged, with one range syntax and
  strict rejection of malformed operators.
- The release workflow runs the same gate as CI: `gofmt`, `go vet`,
  `go mod verify` and `go test -race`, not only `go test`.

### Removed

- The `--no-policy` bypass on `install` and `upgrade`. A policy change is a
  reviewed file change; CI and local commands enforce the same file.

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
  `--update` re-resolves and writes the result.
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
