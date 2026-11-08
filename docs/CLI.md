# also command reference

Run `also <command> --help` for every flag. Natural forms are the primary
interface; flags exist for scripts.

`also` is pre-1.0. The public surface is intentionally small: four core
commands, a setup path, two inspection commands and one maintenance command.
Read-only views are flags on those commands, not separate verbs.

```
Core workflow:  install  push  audit  uninstall
Setup:          setup  init  connect  adopt  upgrade  tag
Inspect:        list  show
Maintenance:    doctor
```

Advanced commands (`version`, `config`, `exclude`, `include`, `policy ...`,
`reshape`, `add harness`, `remove harness`) remain callable but are hidden from
`also --help`.

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

There is no self-update command. Re-run the installer, or use the package
manager or `go install` path that installed the binary.

## Namespaces

```sh
also connect personal git@github.com:me/ai-infra.git
also connect personal git@github.com:me/ai-infra.git --migrate
also connect personal          # choose an existing namespace as default
also connect                   # list namespaces
```

A namespace is a name plus a git URL. There is no user-managed local set; the
clone under `~/.also/cache/` is an implementation detail. `--namespace` selects
a namespace for commands that take one.

## Core workflow

### Install

```sh
also install <package> [for <harness>] [from <namespace>] [flags]
also install --frozen
also install <part> <kind>... from <package> [for <harness>] [from <namespace>]
```

`install` decodes a package into the native layout of one harness. Instructions
become a managed block; skills, commands and agents become files; MCP servers,
hooks and portable settings are merged into native config with reversible
bookkeeping.

Installs target the nearest project root: the closest `also.lock` or
`also.json`, otherwise the git root. Running from `internal/...` therefore does
not create a second project. Use `--dir` to choose another project explicitly.

The harness comes from `for <harness>`, then from the layout present in the
project, then from the configured default.

Useful flags:

- `--except <kind|name>` removes a kind or a named component from a complete
  install, for example `--except instructions --except settings --except mcp`.
- `--only` replaces the previous part selection instead of adding to it.
- `--latest` does not pin the resolved semantic version.
- `--force` overwrites conflicting or modified files; a backup is kept.
- `--no-config` skips MCP servers, hooks and settings.
- `--no-gitignore` leaves `.gitignore` untouched. By default `install` adds the
  managed harness block, so generated files stay out of the product history.
- `--frozen` replays `also.lock` exactly. If the namespace is not configured,
  the remote recorded in the lock is used, so CI needs no separate setup.

Selection is additive by default: `also install agents from p` keeps the skills
already installed. A plain `also install <package>` installs the whole package
again, resetting a previous partial selection.

### Push

```sh
also push [directory] [--semver V | --bump major|minor|patch] [--dry-run] [--json]
```

`push` publishes the directory marked by `also.json`. The marker records the
package name and source harness, so no path or harness is needed. The lookup
stops at the git root (or the home directory), so a marker in `$HOME` cannot
claim every subdirectory. Files ignored by `.gitignore` are excluded. A package
with no installable component is refused unless `--allow-empty` is passed, and a
source instruction file that already contains an also-managed block must be
captured with `adopt` instead. Agent, command and skill frontmatter is parsed
before anything is published: an unquoted YAML value such as
`description: Owns verification: smoke checks` fails `push` instead of failing
later on the first cross-harness install. Quote values that contain `: `.

### Audit

```sh
also audit [--all] [--json] [--ci] [--strict] [--sbom <path>]
also audit --drift [--all] [--json] [--dir <path>]
also audit --verify [--namespace N] [--refresh] [--json]
also audit --policy [--json] [--dir <path>]
```

The default report is the inventory: namespace, content version, full digest,
harness, source harness, parts, exclusions, file count, missing and modified
files, managed blocks, config drift and every executable component (MCP
servers, hooks, plugins and commands). It never downloads a package.

`--drift` prints only the files, blocks and config entries that differ.
`--verify` checks namespace manifests, digests, version tags and install
references. `--policy` prints the effective `also.policy.yml` and its signature
status. Choose one mode at a time.

A modified file is a warning, because the edit may be intentional. A missing
file, managed instruction block or configuration entry is a failure. `--drift`
and `--verify` exit 1 on any failure without needing `--ci`. The full report
exits 1 on a failure only with `--ci`, which also requires a git-tracked
`also.lock` when the project is
inside git; state in `$HOME` is only a cache. It also fails when the lock pins a
package that is not installed, when the installed version or digest differs,
when an install is missing from the lock, when an installed file, managed block
or managed config file is tracked by git (`infra-tracked`, `block-tracked`,
`config-tracked`), or when a tracked file under a harness directory is not
owned by an install (`stale-infra`). `--strict` also exits 1 on warnings; shared
identical files, blocks and config entries are informational and do not fail
it. A tracked harness config that cannot be parsed is a failure, not a silent
pass, and config/instruction names are compared case-insensitively. `--sbom`
writes CycloneDX 1.5.

### Uninstall

```sh
also uninstall <package> [for <harness>] [--namespace N] [--all] [--dir <dir>] [--json]
```

Uninstall reverses the files, managed block and config edits the install still
owns. Shared resources are left in place with a warning. A locally modified file
or block is refused unless `--force` is passed; a backup is kept before
overwriting. The lock entry is removed after a successful uninstall.

## Setup

```sh
also setup [--yes] [--harness H] [--namespace N=URL] [--default-namespace N]
also init <name> [for <harness>] [--json]
also connect <name> [<git-url>] [--default] [--force] [--migrate] [--yes]
also adopt [directory] [for <harness>] [--name N] [--semver V] [--into <dir>]
also upgrade [package...] [--check] [--major] [--json]
also tag <package> <semver> [version] [--move]
```

`init` writes `also.json` into the current directory. `adopt` captures a live
installation, including preserved native layers, and can push the result.
`upgrade` upgrades the packages installed in the current project; a major move
requires `--major`, and a downgrade is refused. `tag` creates an immutable
semantic alias over a content version; `--move` is the explicit retarget path.

## Inspect

```sh
also list [query] [--versions] [--installs] [--tree] [--harnesses] [--json]
also show <package>[@version] [from <namespace>] [--json]
```

`list` without arguments lists packages in the active namespace. A query
searches package names, descriptions, labels and versions in every connected
namespace. `--versions` shows every content version and alias, `--installs`
shows every install on the machine with its project, `--tree` shows the
installed files/blocks/config entries, and `--harnesses` lists supported
adapters. `show` prints one manifest and its components.

## Maintenance

```sh
also doctor [--refresh] [--fix] [--json]
```

`doctor` checks git, configuration, adapters, namespace caches and installs.
Stale install records whose project directory no longer exists are pruned
automatically. `--refresh` fetches namespaces first. `--fix` re-clones a dirty
or broken namespace cache.

## Advanced commands

These are hidden from `also --help` but keep their documented meaning:

```sh
also version [--json]
also config [get <key> | set <key> <value>]
also exclude|include infra [for <harness>] [in <dir>]
also exclude|include you
also policy keygen --private <path> [--public <path>]
also policy trust --key <public-key> [--name <name>] [--list] [--remove <key|name>]
also policy sign --key <private-key> [--dir <path>]
also policy verify [--ci] [--json]
also reshape <harness> [package...] [--dir <path>|--all] [--json]
also add harness <file|url>
also remove harness <id>
```

`exclude infra for <harness>` merges that harness's paths into the managed
block; it never removes another harness that is still installed. `include infra
for <harness>` removes only entries that no remaining installed harness needs.
`include infra` without `for` removes the whole block. `install`, `upgrade` and
`reshape` call the same logic automatically.

Policy files are enforced from the project up to the git root. A nested
`also.policy.yml` can add restrictions but cannot loosen a root deny, executable
gate or signature requirement, and its added approvals/roles must be satisfied
by the merged signature set; `audit --policy` shows the merged effective policy
and the nearest file.

`reshape` re-materializes this project's packages for another harness, updates
the lock and the managed ignore block, preflights every package before writing,
and rolls earlier packages back if a later write fails. `--all` is the explicit
machine-wide form.

`policy` signs and trusts the governance file; the read-only view is
`also audit --policy`. The trust anchor lives outside the policy
(`~/.also/trusted-policy-keys` or `ALSO_TRUSTED_POLICY_KEYS`).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | any error: invalid usage, failed check, network/git error, refused destructive action |

Errors print to stderr. `--json` output goes to stdout. A failed `doctor` or
`audit` still prints all checks/findings before exiting 1.

## JSON output

Mutating commands accept `--json` and print one JSON value on stdout:

| Command | Shape |
|---|---|
| `init` | name, harness, file |
| `install` | package, version, harness, root, namespace, parts, exclude, files, config changes |
| `push` / `adopt` | package, version, digest, harness, files, warnings, namespace |
| `upgrade` | array of package, harness, old, new, status, error |
| `reshape` | array of package, from, to, status, warnings, error |
| `uninstall` | array of namespace, package, version, harness, warnings |
| `list` | namespace, remote, packages (or query/results for search, installs for `--installs`, tree for `--tree`) |
| `show` | package manifest and components |
| `audit` | schema, project, packages, findings, summary, lock, policy_file |
| `audit --drift` | schema, project, generated_at, findings, summary |
| `audit --verify` | array of health checks |
| `audit --policy` | file, exists, namespaces, packages, executables, limits, governance, signature |
| `doctor` | array of health checks |
| `version` | version, commit, built |

The audit, drift and SBOM schemas are versioned (`also.audit/v1`). Unknown
fields are ignored; adding optional fields keeps the schema stable.
