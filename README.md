<p align="center">
  <img src="docs/assets/hero.svg" alt="also — version once, run anywhere. One portable package rendered for every harness." width="100%">
</p>

<p align="center">
  <strong>Version once. Run anywhere.</strong>
</p>

`also` is the control plane for AI agent configuration. The project is at `0.1.1`: the command surface is deliberately small and still pre-1.0, while package, state and lock schemas are versioned independently. One Go binary installs, versions, audits and reverses instructions, skills, commands, agents, hooks, Model Context Protocol servers and settings across harnesses. It does not just generate files: it remembers what it owns, proves what is installed, reports what drifted, and refuses to act outside your policy.

## 1. The Problem

Every harness invented its own layout for the same ideas. One expects `CLAUDE.md`, another reads `AGENTS.md`, a third uses `QWEN.md`, and each keeps skills, commands, agents, hooks, Model Context Protocol servers and permission settings in a different place. The names, file formats and merge rules rarely match.

The usual result is duplication. The same review skill is copied into four projects. A useful hook exists only in the harness where it was written. A permission change is made in one configuration file and forgotten in the other three. Nothing has a meaningful version, so rolling back means remembering which copy was good.

Formatting is becoming standardized: `AGENTS.md` is read natively by most agents, `SKILL.md` moved to the Agentic AI Foundation, and Model Context Protocol is common. Ownership, drift, policy and rollback are not standardized. Those are the parts that break silently, and they are what `also` is built around:

- one portable package describes the infrastructure once;
- adapters translate it into each harness layout, and skip what cannot be translated;
- every package version is content-addressed, immutable and stored in git;
- installation is reversible per file, per managed instruction block and per configuration entry;
- the same package can live in several harnesses inside one directory without collisions;
- `also audit` reports what is installed, where it came from, what can execute and what changed;
- `also.lock` and semantic-version aliases make a directory reproducible;
- `also.policy.yml` gives a team a gate for untrusted sources and executable components.

## 2. Quick Start

Run one command:

```sh
curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
```

The installer detects the operating system and architecture, downloads the latest release from GitHub, verifies its SHA-256 checksum, verifies the published build attestation when the GitHub CLI is authenticated, installs the binary into `~/.local/bin`, and immediately launches the interactive setup. Setup detects installed harness command-line interfaces, asks for a default harness, connects one or more remote namespaces, and optionally installs a starter package.

On Windows, use PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/seinsdieum/also/main/install.ps1 | iex"
```

For unattended machines, set `ALSO_NO_SETUP=1` and provide setup flags:

```sh
curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | ALSO_NO_SETUP=1 sh
also setup --yes --harness opencode \
  --namespace personal=git@github.com:you/ai-infrastructure.git \
  --default-namespace personal
```

You can also install with Go:

```sh
go install github.com/seinsdieum/also@latest
also setup
```

Initialize the directory that holds your infrastructure:

```sh
cd my-infrastructure
also init code-review for claude
also push --semver 0.1.0
```

`also init` writes `also.json` with the package name and source harness. From then on `also push` publishes that directory and needs no path, no name and no harness. A directory without `also.json` is refused, so you cannot publish an unrelated working directory by accident. Inside a git repository `push` honors `.gitignore`; `also push --dry-run` lists every file before anything is sent, and files outside the harness layout need `--all-files`.

Install the package into another directory, then prove the result:

```sh
cd ../my-project
also install code-review for opencode
also audit
```

`also install` writes `also.lock` next to the project. Commit it. Another machine or a continuous-integration job can then install exactly the same content:

```sh
also install --frozen
```

Capture infrastructure that already exists in a harness layout:

```sh
cd ../existing-project
also init existing-rules for claude
also adopt for claude --semver 0.1.0
```

`adopt` already publishes a version, so the semantic tag belongs on the same
command. Running `push --semver` afterwards creates a second version with the
same content but no description.

Keep the package and the tool current from the directory where it is installed:

```sh
cd ../my-project
also upgrade
```

`also upgrade` upgrades the packages installed in the current project, respecting constraints recorded in `also.lock`. A major move needs `--major`. There is no self-update command: re-run the installer, or use the package manager or `go install` path that installed the binary.

## 3. What also Does

### Initialization

`also init <name> [for <harness>]` writes `also.json` into the current directory. The file records the package name and the source harness, and it is never published inside the package. Every command that publishes, captures or upgrades infrastructure uses that marker, so the working directory is the only argument you need:

```sh
also init code-review for claude
also push
also adopt for claude
also upgrade
```

If the harness is omitted, `also init` uses the layout already present in the directory and then the configured default harness.

### Namespaces

A namespace is a remote git repository that stores packages. It is only a name and a remote uniform resource locator; the local clone is an internal cache. Connect one during setup or add it later:

```sh
also connect personal git@github.com:you/ai-infrastructure.git
also connect personal          # choose the default namespace
```

Choose a namespace per operation with `from <namespace>`:

```sh
also install code-review for opencode from personal
also show code-review from personal
also list review
```

`also list <query>` searches every connected namespace. It is a registry for namespaces you already trust, not a public catalog; there is no service to run and no telemetry.

### Versions, semantic aliases and the lockfile

A package is a directory tree inside a namespace with one live version and immutable version tags. The version identifier is the first sixteen hexadecimal characters of a content digest, so identical content always produces the same version and different content can never reuse an old one. Pin a version with `package@version` when a moving latest version is not appropriate.

Content hashes are good for integrity and awkward for humans. `also` adds semantic-version aliases on top without weakening the content address:

```sh
also push --semver 0.1.0          # tag this exact content as 0.1.0
also push --bump patch            # derive the next alias from the highest one
also tag code-review 0.1.1        # create an alias on an existing version
also tag code-review 0.1.1 --move # explicitly move an existing alias
also install 'code-review@^0.1'   # resolve the highest matching alias
```

The installed version remains the digest. The alias only answers "give me the latest 1.x". Aliases are immutable by default; moving one requires `--move`, and the move refuses to overwrite a remote change this clone has not seen.

`also install` writes `also.lock`, a small JSON file that pins the package, namespace, content version, digest, harness, parts, exclusions and the original constraint. Keep it in git. The first install pins `^major.minor.patch` when the version has a semantic alias; `--latest` opts out. `also install --frozen` refuses to resolve anything new, and `--update` ignores the pins for one command while still writing the result. A fresh checkout does not need a namespace setup step: the lock records the remote.

### Self-hosting one repository

Keep the team in an infrastructure repository and the product in another. The
product commits only `.gitignore`, `also.lock`, `also.policy.yml` and
`also.policy.sig`; the harness files are generated on each machine and in CI:

```sh
cd product
git init
also exclude infra for opencode   # explicit; the project layout wins over a machine default
also exclude you
also install team for opencode
git add .gitignore also.lock also.policy.yml also.policy.sig
```

If the product owns its own `AGENTS.md` or `opencode.json`, exclude the managed
parts instead of handing over the whole file:

```sh
also install team for opencode --except instructions --except settings --except mcp
```

A plain `also install team` restores the whole package. Adding selectors is
additive (`also install agents from team` keeps existing skills); `--only`
replaces the selection. `also audit --ci` fails when any installed
file, managed block or managed config file is tracked by git, so the
artifact-free rule is an invariant rather than a convention.

### Audit, drift and the executable surface

`also audit` is the part no formatter gives you. It answers:

- which package and namespace produced this directory;
- which content version and full digest are installed;
- which files, instruction blocks and configuration entries still match;
- which components can execute code or start a process: Model Context Protocol servers, hooks, plugins and commands;
- whether the installation still satisfies `also.policy.yml`.

```sh
also audit
also audit --json
also audit --ci                  # exit 1 on a missing file, block or config entry
also audit --strict              # exit 1 on any finding, including user edits
also audit --sbom also.cdx.json  # CycloneDX 1.5 inventory
also audit --drift               # only the differences
also audit --verify              # namespace digests, tags and install references
also audit --policy              # the effective also.policy.yml
```

`also audit` never downloads a package; the recorded digest is the evidence. A modified file is a warning because you may have edited it on purpose. A missing file, managed block or configuration entry is a failure because the install is incomplete.

`also audit --ci` additionally requires a git-tracked `also.lock` and compares every entry with the local state: a lock entry that is not installed, a version or digest mismatch, an unexpected extra install, or a missing lock is a failure. It also fails when an installed file, managed instruction block or managed config file is tracked by git (`infra-tracked`, `block-tracked`, `config-tracked`) or when a tracked file sits under a harness component directory without an owner (`stale-infra`), which turns the artifact-free layout into a CI invariant. The lock is the committed source of truth; `~/.also/state.json` is the per-machine cache. That is what makes the CI gate meaningful even if the local state was edited.

### Policy

A project can commit `also.policy.yml`:

```yaml
schema: also.policy/v1
namespaces:
  allow: [personal, work]
  deny: [untrusted]
packages:
  allow: []
  deny: [experiment, "scratch/*"]
executables:
  allow_hooks: true
  allow_mcp: true
  allow_plugins: false
  allow_commands: true
limits:
  max_package_bytes: 67108864
  allow_literal_secrets: false
```

The policy can require approvals and roles:

```yaml
governance:
  require_signature: true
  required_approvals: 2
  required_roles: [security, maintainer]
  approvers:
    - name: alice
      role: security
      key: <base64 Ed25519 public key>
    - name: bob
      role: maintainer
      key: <base64 Ed25519 public key>
```

`also audit --policy` shows the effective policy. Every `also.policy.yml` from the project up to the git root is enforced, so a nested policy can add restrictions but cannot disable a root deny or signature requirement. `also policy keygen`, `also policy trust`, `also policy sign` and `also policy verify --ci` manage the detached Ed25519 signature in `also.policy.sig`; they remain available as advanced commands. The trusted public keys live outside the policy (`~/.also/trusted-policy-keys` or `ALSO_TRUSTED_POLICY_KEYS`), so a changed policy or approver list cannot make itself trusted. `docs/GOVERNANCE.md` has the full workflow.

`also install` and `also upgrade` enforce the file before writing anything. `also audit --ci` re-checks the inventory and fails when a policy exists but is unsigned, when a signature is invalid or untrusted, when a required role or approval is missing, and when an installed package violates the rules. There is no bypass flag: changing the policy is a reviewed, signed file change.

### Adapters and cross-harness architecture

An adapter describes where a harness keeps each component and how it encodes configuration. A built-in adapter is ready to install without compiling code. A community adapter is declarative JSON and reuses an existing encoding.

| Adapter | Instructions | Skills | Commands and agents | Hooks and settings | Model Context Protocol |
|---|---|---|---|---|---|
| Claude Code | `CLAUDE.md` | `.claude/skills` | yes | yes | yes |
| Codex CLI | `AGENTS.md` | `.agents/skills` | not supported by the harness | project layers require a trusted project | yes |
| OpenCode | `AGENTS.md` | `.opencode/skills` | yes | yes | yes |
| Qwen Code | `QWEN.md` | `.qwen/skills` | yes | yes | yes |

Cross-harness translation is deliberately honest. A component that has no equivalent in the target harness is skipped with a warning instead of being written into a file the harness ignores. Native data that cannot be translated is preserved in `.also-layers/` so a later round trip can restore it.

Add a community adapter without recompiling (advanced command):

```sh
also add harness ./harness.json
also add harness https://example.com/harness.json --sha256 <digest>
```

Adapters are declarative. They cannot execute code, every install path stays inside the project root, and downloaded specifications can be pinned to an exact checksum. `global_base` is only a detection hint for `also setup`.

### Installation and removal

`also` remembers every created file, managed instructions block and configuration edit in `~/.also/state.json`. Uninstalling removes only what the package still owns. If another install shares a file, block or config entry, it is left in place with a warning. If you edited a file, `also` refuses to delete it unless you pass `--force`, and it keeps a backup before overwriting anything. A backup failure aborts the transaction instead of silently losing the original.

Install only the parts you want:

```sh
also install review skill from code-review for opencode
also install mcp from code-review
```

Supported selectors include `instructions`, `skills`, `commands`, `agents`, `plugins`, `hooks`, `mcp`, `settings` and adapter-defined component kinds.

### Main commands

Core workflow:

| Command | Purpose |
|---|---|
| `also install <package> [for <harness>] [from <namespace>]` | Materialize a package into a project and update `also.lock`. |
| `also push [directory] [--semver V] [--bump kind]` | Publish the initialized infrastructure. |
| `also audit [--drift\|--verify\|--policy]` | Inventory, differences, namespace verification or the effective policy. |
| `also uninstall <package> [--namespace N]` | Remove an installation and reverse its configuration edits. |

Setup and inspection:

| Command | Purpose |
|---|---|
| `also setup` | Interactive first-run setup with harness and namespace choices. |
| `also init <name> [for <harness>]` | Mark the current directory as portable infrastructure. |
| `also connect <name> [<git-url>]` | Connect a namespace, or choose an existing one as default. |
| `also adopt [directory] [for <harness>]` | Capture the initialized infrastructure and publish a new version. |
| `also upgrade [--major]` | Upgrade packages installed in the current project. |
| `also tag <package> <semver> [version]` | Point a semantic alias at a content version. |
| `also list [query] [--versions\|--installs\|--tree\|--harnesses]` | List packages, search, show installs, the installed tree or adapters. |
| `also show <package>[@version] [from <namespace>]` | Show a package manifest and its components. |
| `also doctor [--fix]` | Check and repair git, configuration, adapters, caches and installs. |

Advanced commands (`version`, `config`, `exclude`, `include`, `policy ...`,
`reshape`, `add harness`, `remove harness`) remain callable but are hidden from
`also --help`. The complete command reference, JSON output shapes and exit codes
live in [docs/CLI.md](docs/CLI.md).

## 4. Design principles

`also` is built around a small set of rules. They explain both what the tool does and what it deliberately does not do.

**Ownership over rendering.** Writing a file is easy. Knowing which tool created which line, which configuration entry, and which other package still needs it is the hard part. Every install records files, managed instruction blocks and configuration edits separately, so removal is precise and shared resources are preserved.

**Evidence over promises.** `also audit` reports the installed version, full digest, source namespace, file state, drift and executable surface. A package is never downloaded just to prove what is already installed; the recorded digest is the evidence.

**Content address first, semantic version as an alias.** The version is the content digest. A semantic alias is an immutable label on top, so `^2.1` works without giving up immutability or reproducibility; an explicit `--move` is the only way to retarget one.

**One binary, no service.** Namespaces are ordinary git repositories. There is no control plane to host, no account to create, and no telemetry. A lockfile and a policy file are the only project artifacts.

**Honest translation.** If a component has no equivalent in the target harness, it is skipped with a warning instead of being written somewhere the harness ignores. Native data that cannot be translated is preserved in `.also-layers/` for a later round trip.

**Explicit scope.** `also` ships four built-in harnesses: Claude Code, Codex CLI, OpenCode and Qwen Code. It does not try to be a public package catalog, and it does not sandbox what a hook or command does when the harness later runs it. Adapters are declarative so the set can grow without weakening the ownership model.

## 5. Contributing

Contributions are welcome. The project is written in Go and targets Go 1.23 or newer.

```sh
git clone https://github.com/seinsdieum/also.git
cd also
make check          # formatting, vet and tests
make build          # build bin/also
make smoke          # exercise the real harness command-line interfaces, if installed
```

`make check` runs `gofmt`, `go vet` and the full test suite with the race detector in continuous integration. The four-by-four harness matrix in `test/e2e/matrix_test.go` runs on every platform, and the release workflow runs the test suite before publishing. The `make smoke` target is optional locally because it needs the real harness binaries; it never calls a model.

Useful documents:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the package, adapter and rendering model;
- [docs/ADAPTERS.md](docs/ADAPTERS.md) for adding a harness without recompiling;
- [docs/PACKAGE_FORMAT.md](docs/PACKAGE_FORMAT.md) for the package contract;
- [docs/CONTRACTS.md](docs/CONTRACTS.md) for schema stability and migration policy;
- [examples/control-plane](examples/control-plane) for a copyable repository layout;
- [docs/CI.md](docs/CI.md) for the lockfile, audit and policy workflow in CI;
- [docs/GOVERNANCE.md](docs/GOVERNANCE.md) for policy keys, roles and approvals;
- [docs/RELEASING.md](docs/RELEASING.md) for the release checklist and version policy;
- [docs/CLI.md](docs/CLI.md) for the command reference.

Before opening a pull request, run `make check`, keep user-facing text in complete English sentences, and add a test for any new branch, parser, security check or installer path. Adapter changes must keep the declarative path bounds and must not execute code. Security issues should go through the private channel described in [SECURITY.md](SECURITY.md), not through a public issue.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.

`also` is built with the Go programming language, the Cobra command-line framework, the BurntSushi TOML library and the `yaml.v3` library. It exists because Claude Code, Codex CLI, OpenCode and Qwen Code each publish a thoughtful but different configuration model. Thank you to every contributor and to the maintainers of those projects and libraries.

## 6. License

`also` is released under the [Apache License 2.0](LICENSE). You may use it in personal and commercial products.

Copyright 2026 Aliaksandr Sialitski. The license keeps authorship with the project and requires that redistributions preserve the copyright notice and the [NOTICE](NOTICE) file. If you embed `also` inside another product, credit the project in that product's credits or documentation. The NOTICE file records the exact attribution text.

Third-party dependencies remain under their own licenses.
