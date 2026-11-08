# also command reference

Run `also <command> --help` for every flag. Natural forms are the primary
interface; flags exist for scripts.

## First run

```sh
curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
```

The installer verifies the release checksum, installs the binary, and starts
`also setup`. For unattended machines set `ALSO_NO_SETUP=1` and pass flags:

```sh
also setup --yes --harness opencode \
  --namespace personal=git@github.com:me/ai-infrastructure.git \
  --default-namespace personal
```

`setup` detects installed harness command-line interfaces and configuration
directories, writes `also.config/v2`, validates every namespace before saving,
and can install a starter package with `--install <package>`. When the script is
piped through `sh`, prompts are read from the terminal device, so the
interactive setup still works.

## Updates

```sh
also update --check
also update
```

`also update` downloads the latest GitHub release, verifies `SHA256SUMS`, and
replaces the running binary on macOS and Linux. On Windows it writes
`also.exe.new` next to the current executable because a running file cannot be
replaced. Ordinary commands check the latest release at most once per day and
print a single notice on standard error when an update is available. Set
`ALSO_NO_UPDATE_CHECK=1` to disable that check.

## Namespaces

```sh
also connect personal git@github.com:me/ai-infra.git
also connect personal git@github.com:me/ai-infra.git --migrate
also connect
also use personal
```

A namespace is a name plus a git URL. There is no user-managed local set; the
clone under `~/.also/cache/` is an implementation detail. `--namespace` selects
a namespace for commands that take one.

## Initialization

```sh
cd my-infrastructure
also init <name> [for <harness>] [--json]
```

`also init` writes `also.json` into the current directory. The marker records
the package name and source harness, and it is excluded from published
packages. The harness comes from `for <harness>`, then from the layout present
in the directory, then from the configured default.

`also push` and `also adopt` require this marker. A directory without it is
refused, so an unrelated working directory cannot be published by accident.

## Packages

```sh
also list [--refresh] [--versions] [--json]
also search [query] [--refresh] [--json]
also show <package>[@version] [from <namespace>] [--json]
also push [directory] [--semver V | --bump major|minor|patch] [--dry-run] [--all-files] [--json]
also tag <package> <semver> [version] [from <namespace>]
also adopt [directory] [for <harness>] [--name N] [--into <dir>] [--json]
```

`also push` publishes the nearest initialized directory. With no argument it
starts from the current directory, reads the name and harness from `also.json`,
and needs no path. The lookup stops at the git root (or the home directory), so
a marker in `$HOME` cannot claim every subdirectory. Inside a git repository,
files ignored by `.gitignore` are excluded. Before publishing, `--dry-run`
lists every file that would be included; files outside the source harness
layout are refused unless `--all-files` is passed. `adopt` extracts the
initialized directory's harness-native files into a portable bundle and
publishes it straight to the namespace. Without `--into` the extracted bundle
is temporary. `--no-push` keeps the bundle and therefore requires `--into`.

`also search` reads package names and descriptions from every connected
namespace. It is not a public catalog and sends no telemetry.

Content versions stay canonical. `--semver` and `--bump` create a mutable
semantic alias over the immutable content tag; `also tag` moves an alias to an
existing version. Installs accept exact versions, prefixes, labels and semantic
ranges such as `^1.4` and `>=1.2 <2.0`.

### The lockfile

`also install` writes `also.lock` into the project root. It records the package,
namespace, remote, content version, full digest, original constraint, harness,
parts and whether configuration was installed. Commit it.

- `also install --frozen` requires an entry and fails if anything would resolve
  differently.
- `also install --update` ignores the lock for one command.
- `also install --no-lock` neither reads nor writes the file.
- `also upgrade` respects the recorded constraint and updates the lock after a
  successful upgrade.
- `also uninstall` removes its entry.

The file is local desired state, not a replacement for `~/.also/state.json`,
which records what was actually written on this machine.

## Installs

```sh
also install <package> [for <harness>] [from <namespace>] [--frozen] [--update] [--no-lock] [--no-policy] [--json]
also install <part> <kind> from <package> [for <harness>] [from <namespace>]
also upgrade [package...] [--check] [--no-lock] [--json]
also reshape <harness> [package...] [--json]
also tree [for <harness>] [--json]
also uninstall <package> [for <harness>] [--all] [--json]
```

Installs always target the current directory. One package can be installed
into several harnesses in the same directory. `also upgrade` upgrades the
packages installed in the current directory, not packages elsewhere.
`also uninstall` defaults to the current directory; use `--dir <dir>` to
choose another one.

A project policy (`also.policy.yml`) is enforced before an install or upgrade
writes anything. Use `--no-policy` only for local recovery; continuous
integration should keep the policy on.

## Audit, drift and policy

```sh
also audit [--all] [--json] [--ci] [--strict] [--sbom <path>]
also drift [--all] [--json]
also policy [--json]
```

`also policy` prints the effective file when it exists, or says that the
project has no policy. It does not change anything.

`also audit` reports the installed packages, namespace, content version, full
digest, harness, source harness, parts, recorded file count, missing and
modified files, managed block state, configuration drift and every executable
component: Model Context Protocol servers, hooks, plugins and commands. It never
downloads a package.

A modified file is a warning, because the edit may be intentional. A missing
file, managed instruction block or configuration entry is a failure. `--ci`
exits 1 on a failure; `--strict` exits 1 on any finding. `drift` prints only the
differences.

`--sbom` writes a CycloneDX 1.5 JSON document for the inventory. It contains one
application component per installed package with the full SHA-256 digest and
properties for namespace, harness, source harness, file count and parts.

A project policy is a YAML file:

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

Allow lists are allowlists when non-empty; deny always wins. Package entries
support `path.Match` globs. Executable kinds default to allowed when omitted.
The size limit defaults to 64 MiB per package. Literal secrets are detected in
JSON, YAML, TOML and plain text.

## Safety and diagnosis

```sh
also doctor [--refresh] [--json]
also verify [namespace] [--refresh] [--json]
also repair <namespace|--all> [--yes]
also add harness <file|url> [--sha256 <hash>]
also remove harness <id>
also exclude|include infra [for <harness>] [in <dir>]
also exclude|include you
```

`doctor` and `verify` never change data. They exit non-zero on a failed check.
`repair` deletes the internal cache clone for a namespace and fetches it again;
user bundles and installed files are untouched.
`verify` reads every package version in every namespace and confirms installed
state still points at existing versions.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | any error: invalid usage, failed check, network/git error, refused destructive action |

Errors print to stderr. `--json` output goes to stdout. A failed `doctor` or
`verify` still prints all checks before exiting 1.

## JSON output

Mutating commands accept `--json` and print one JSON value on stdout:

| Command | Shape |
|---|---|
| `init` | name, harness, file |
| `install` | package, version, harness, root, namespace, files, config changes |
| `push` / `adopt` | package, version, digest, harness, files, warnings, namespace |
| `upgrade` | array of package, harness, old, new, status, error |
| `reshape` | array of package, from, to, status, error |
| `uninstall` | array of namespace, package, version, harness, warnings |
| `audit` | schema, project, packages, findings, summary, policy_file |
| `drift` | schema, project, findings, summary |
| `policy` | file, exists, namespaces, packages, executables, limits |
| `search` | query, results with namespace, package, version, description |
| `tag` | human confirmation on stdout; no JSON mode |
| `setup` | harness, default_namespace, namespaces, installed |
| `doctor` / `verify` | array of name, status (`ok`/`warn`/`fail`), detail |
| `update` | current, latest, updated, reinstall, manual_path, asset, attestation |
| `version` | version, commit, built |
| `list` / `show` / `tree` | existing package/tree objects |

`audit --sbom -` writes the SBOM to stdout, so it cannot be combined with
`--json`.

Warnings never include secret values, only key locations.

## Configuration

```sh
also config
also config get default_namespace
also config set default_namespace personal
also config set default_harness opencode
```

`ALSO_HOME` overrides `~/.also`. `ALSO_ADAPTERS` overrides the community
adapter directory.
