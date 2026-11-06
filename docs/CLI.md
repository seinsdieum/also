# also command reference

Run `also <command> --help` for every flag. Natural forms are the primary
interface; flags exist for scripts.

## First run

```sh
also setup
also setup --yes --harness opencode \
  --namespace personal=git@github.com:me/ai-infra.git \
  --default-namespace personal
```

`setup` detects installed harness CLIs/config directories, writes
`also.config/v2`, validates every namespace before saving, and can install a
starter package with `--install <package>`.

## Namespaces

```sh
also connect personal git@github.com:me/ai-infra.git
also connect personal git@github.com:me/ai-infra.git --migrate
also connect
also use personal
```

A namespace is a name plus a git URL. There is no user-managed local set; the
clone under `~/.also/cache/` is an implementation detail. The old spelling
`also set <name> --remote <url>` still works.

## Packages

```sh
also list [--refresh] [--versions] [--json]
also show <package>[@version] [from <namespace>] [--json]
also push <bundle-dir> [for <harness>] [--name N] [--label v1] [--json]
also adopt <project-dir> [for <harness>] [--name N] [--into <dir>] [--json]
```

`adopt` publishes straight to the namespace. Without `--into` the extracted
bundle is temporary. `--no-push` keeps the bundle and therefore requires
`--into`.

## Installs

```sh
also install <package> for <harness> [--json]
also install <part> <kind> from [namespace@]<package> [for <harness>]
also upgrade [package...] [--check] [--json]
also reshape <harness> [package...] [--json]
also tree [for <harness>] [--json]
also uninstall <package> [for <harness>] [--all] [--json]
```

Project scope is the default. `--scope global` writes to the harness's
user-wide config. One package can be installed into several harnesses in the
same project.

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
| `install` | package, version, harness, scope, root, namespace, files, config changes |
| `push` / `adopt` | package, version, digest, harness, files, warnings, namespace |
| `upgrade` | array of package, harness, scope, old, new, status, error |
| `reshape` | array of package, from, to, scope, status, error |
| `uninstall` | array of namespace, package, version, harness, scope, warnings |
| `setup` | harness, scope, default_namespace, namespaces, installed |
| `doctor` / `verify` | array of name, status (`ok`/`warn`/`fail`), detail |
| `version` | version, commit, built |
| `list` / `show` / `tree` | existing package/tree objects |

Warnings never include secret values, only key locations.

## Configuration

```sh
also config
also config get default_namespace
also config set default_namespace personal
also config set default_harness opencode
also config set default_scope project
```

`ALSO_HOME` overrides `~/.also`. `ALSO_ADAPTERS` overrides the community
adapter directory.
