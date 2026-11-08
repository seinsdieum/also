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
also list [--refresh] [--versions] [--installs] [--json]
also search [query] [--refresh] [--json]
also show <package>[@version] [from <namespace>] [--json]
also push [directory] [--semver V | --bump major|minor|patch] [--dry-run] [--all-files] [--allow-empty] [--json]
also tag <package> <semver> [version] [from <namespace>] [--move]
also adopt [directory] [for <harness>] [--name N] [--semver V | --bump kind] [--into <dir>] [--json]
```

`also push` publishes the nearest initialized directory. With no argument it
starts from the current directory, reads the name and harness from `also.json`,
and needs no path. The lookup stops at the git root (or the home directory), so
a marker in `$HOME` cannot claim every subdirectory. Inside a git repository,
files ignored by `.gitignore` are excluded. Before publishing, `--dry-run`
lists every file that would be included; files outside the source harness
layout are refused unless `--all-files` is passed. A package with no installable
component is refused unless `--allow-empty` is passed, so a `.gitignore` that
hides the layout cannot silently publish a broken version. A source instruction
file that already contains an also-managed block is refused: use `adopt` to
capture a live installation, or remove the block first. `adopt` extracts the
initialized directory's harness-native files into a portable bundle and
publishes it straight to the namespace. Without `--into` the extracted bundle
is temporary. `--no-push` keeps the bundle and therefore requires `--into`.
`adopt` already publishes, so put the semantic tag on the same command
(`also adopt --semver 1.0.0`) instead of re-pushing the directory afterwards.

`also list --installs` lists every install on the machine with namespace,
package, version, harness and project path. It is the multi-project view; the
package table shows one location per package.

`also search` reads package names and descriptions from every connected
namespace. It is not a public catalog and sends no telemetry.

Content versions stay canonical. `--semver` and `--bump` create an immutable
semantic alias over the content tag; `also tag` creates an alias on an existing
version, and `also tag --move` is the explicit path for retargeting one. A move
uses a lease and refuses to overwrite a remote change this clone has not seen.
Installs accept exact versions, prefixes, labels and semantic ranges such as
`^1.4` and `>=1.2 <2.0`.

### The lockfile

`also install` writes `also.lock` into the project root. It records the package,
namespace, remote, content version, full digest, original constraint, harness,
parts, exclusions and whether configuration was installed. Commit it.

- A fresh install with no explicit version records `^major.minor.patch` when
  the resolved content version has a semantic alias. `--latest` opts out.
- `also install --frozen` requires an entry and fails if anything would resolve
  differently. If the namespace is not configured, the remote recorded in the
  lock is used, so CI needs no separate `also setup`. Two entries that write the
  same config key or file differently are rejected before the first write;
  `--force` overwrites local files but does not make a contradictory lock valid.
- `also install --update` ignores the pins for one command and writes the new
  result to the lock.
- `also upgrade` respects the recorded constraint. Unconstrained installs stay
  inside the installed major unless `--major` is passed.
- `also uninstall` removes its entry; `--namespace` selects one of two installs
  with the same package name from different namespaces.

The file is local desired state, not a replacement for `~/.also/state.json`,
which records what was actually written on this machine. Config origins and
instruction-file provenance live in `~/.also/config-origins`, outside state, so
a same-machine state loss still uninstalls reversibly: `install --frozen`
re-adopts byte-identical files and then restores the original config base and
the created-file decision. A fresh machine without that directory can only
adopt the current values; keep the directory or uninstall before moving if
exact reversal matters.

## Installs

```sh
also install <package> [for <harness>] [from <namespace>] [--frozen] [--update] [--except K] [--only] [--latest] [--json]
also install --frozen            # replay every entry in also.lock
also install <part> <kind>... from <package> [for <harness>] [from <namespace>] [--except K] [--only]
also upgrade [package...] [--check] [--major] [--json]
also reshape <harness> [package...] [--dir <path>|--all] [--json]
also tree [for <harness>] [--json]
also uninstall <package> [for <harness>] [--namespace N] [--all] [--json]
also prune [--dry-run] [--json]
```

Installs target the nearest project root: the closest `also.lock` or
`also.json`, otherwise the git root. Running from `internal/...` therefore does
not create a second project inside the first. The harness comes from
`for <harness>`, then from the layout present in the project, then from the
configured default. Use `--dir` to choose another project explicitly. One package can be installed into several harnesses in the
same directory. `also install --frozen` with no package argument installs every
entry in `also.lock` for the recorded harness, parts and exclusions, which is
the intended CI form.

A plain `also install <package>` installs the whole package again, resetting a
previous partial selection. When selectors are present, they are additive:
`also install agents from p` keeps the skills already installed. `--only`
replaces the selection. `--except` removes a kind or a named component from a
complete install, for example
`also install p for opencode --except instructions --except settings` leaves a
project-owned `AGENTS.md` and `opencode.json` untouched. `also upgrade` upgrades
the packages installed in the current directory, not packages elsewhere;
`--major` is required to cross a major version. `also reshape` is scoped to the
current project, updates `also.lock`, and `--all` is the explicit machine-wide
form. Reshape preflights every package before writing and rolls already moved
packages back if a later write fails; a rollback is best-effort if the
filesystem itself is unwritable. `also uninstall` defaults to the current
directory; use `--dir <dir>` to choose another one. If a project directory was deleted manually, `also prune`
removes its stale state records; it never touches existing projects with
missing files.

A project policy (`also.policy.yml`) is enforced before an install or upgrade
writes anything. There is no bypass flag: to change the policy, edit the file
and review the change like any other committed configuration.

## Audit, drift and policy

```sh
also audit [--all] [--json] [--ci] [--strict] [--sbom <path>]
also drift [--all] [--json]
also policy [--json]
also policy keygen --private <path> [--public <path>]
also policy trust --key <public-key> [--name <name>] [--list] [--remove <key|name>]
also policy sign --key <private-key> [--dir <path>]
also policy verify [--ci] [--json]
```

`also policy` prints the effective file when it exists, or says that the
project has no policy. It does not change anything.

`also policy keygen` creates an Ed25519 key. The private key stays outside the
repository; the base64 public key goes into `governance.approvers`. Trust the
public key with `also policy trust` or `ALSO_TRUSTED_POLICY_KEYS`; a policy
cannot trust its own key. `also policy sign` signs the exact bytes of
`also.policy.yml` and writes `also.policy.sig`. `also policy verify --ci` fails
when signatures, roles or approvals do not satisfy the governance block.

When a policy exists, `audit --ci` also fails on an unsigned policy, an invalid
or untrusted signature, a missing required role and insufficient approvals.
The `also.policy.sig` file is committed with the policy.

`also audit` reports the installed packages, namespace, content version, full
digest, harness, source harness, parts, exclusions, recorded file count, missing
and modified files, managed block state, configuration drift and every
executable component: Model Context Protocol servers, hooks, plugins and
commands. It never downloads a package. With `--all` it prints the project path
for every install.

A modified file is a warning, because the edit may be intentional. A missing
file, managed instruction block or configuration entry is a failure. `--ci`
exits 1 on a failure and requires a git-tracked `also.lock` when the project is
inside git: state in `$HOME` is only a cache, so the committed lock is the
source of truth. A directory outside git has no tracking metadata; `--ci` then
checks only the local lock and files. It also fails
when the lock pins a package that is not installed, when the installed version
or digest differs, when an install is missing from the lock, and when an
installed file, managed instruction block or managed config file is tracked by
git (`infra-tracked`, `block-tracked`, `config-tracked`), or when a tracked file
sits under a harness component directory without being owned by an install
(`stale-infra`). Those findings make the artifact-free rule a CI invariant
instead of a `.gitignore` convention.
`--strict` exits 1 on any warning or failure; shared identical files, blocks
and config entries are informational and do not fail it. `drift` prints only
the differences.

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

`exclude infra for <harness>` merges that harness's paths into the existing
managed block; it never removes another harness that is still installed.
`include infra for <harness>` removes only entries that no remaining installed
harness still needs (a shared `AGENTS.md` stays for Codex), while `include infra`
without `for` removes the whole block. The block belongs to the git root, so a
monorepo keeps one union for all projects under it.

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
| `install` | package, version, harness, root, namespace, parts, exclude, files, config changes |
| `push` / `adopt` | package, version, digest, harness, files, warnings, namespace |
| `upgrade` | array of package, harness, old, new, status, error |
| `reshape` | array of package, from, to, status, error |
| `list --installs` | array of namespace, package, version, harness, project, scope |
| `uninstall` | array of namespace, package, version, harness, warnings |
| `prune` | dry_run, removed array (namespace, package, version, harness, project) |
| `audit` | schema, project, packages, findings, summary, lock, policy_file |
| `drift` | schema, project, findings, summary |
| `policy` | file, exists, namespaces, packages, executables, limits, governance, signature |
| `policy verify` | file, signed, valid, signatures, required, roles, missing_roles, error |
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
