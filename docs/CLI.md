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
also show <package>[@version] [from <namespace>] [--json]
also push [directory] [--json]
also adopt [directory] [for <harness>] [--name N] [--into <dir>] [--json]
```

`also push` publishes the nearest initialized directory. With no argument it
starts from the current directory, reads the name and harness from `also.json`,
and needs no path. `adopt` extracts the initialized directory's harness-native
files into a portable bundle and publishes it straight to the namespace.
Without `--into` the extracted bundle is temporary. `--no-push` keeps the
bundle and therefore requires `--into`.

## Installs

```sh
also install <package> [for <harness>] [from <namespace>] [--json]
also install <part> <kind> from <package> [for <harness>] [from <namespace>]
also upgrade [package...] [--check] [--json]
also reshape <harness> [package...] [--json]
also tree [for <harness>] [--json]
also uninstall <package> [for <harness>] [--all] [--json]
```

Installs always target the current directory. One package can be installed
into several harnesses in the same directory. `also upgrade` upgrades the
packages installed in the current directory, not packages elsewhere.
`also uninstall` defaults to the current directory; use `--dir <dir>` to
choose another one.

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
| `setup` | harness, default_namespace, namespaces, installed |
| `doctor` / `verify` | array of name, status (`ok`/`warn`/`fail`), detail |
| `update` | current, latest, updated, manual_path, asset |
| `version` | version, commit, built |
| `list` / `show` / `tree` | existing package/tree objects |

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
