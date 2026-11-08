# Format contracts and compatibility

`also` freezes the on-disk contracts at 1.0. A breaking change requires a new
schema string and an automatic migration; additive fields are allowed inside a
schema because unknown JSON fields are ignored by older readers.

## Schemas

| Contract | Schema | Status |
|---|---|---|
| Package manifest and origin tree | `also.pkg/v1` | stable |
| User config | `also.config/v2` | stable |
| Install state | `also.state/v1` | supported |
| Namespace manifest | `also.set/v1` | stable |
| Local infrastructure marker | `also.infra/v1` | stable |
| Community adapter spec | `also.adapter/v1` | stable |
| Project lockfile | `also.lock/v1` | stable |
| Project policy | `also.policy/v1` | stable |
| Audit report | `also.audit/v1` | stable |

### Package (`also.pkg/v1`)

The version is the first 16 hex characters of a SHA-256 digest over metadata
plus file paths, modes and content. The algorithm is part of the contract: two
different digests must never describe the same version. A package pushed from
any OS produces the same digest; exec bits come from the git index when the
worktree cannot represent them.

New optional fields may be added. Removing or changing the meaning of an
existing field, the digest algorithm, or the `origin/` layout needs
`also.pkg/v2`.

### Config (`also.config/v2`)

A namespace is a name plus a `remote` URL. The local clone is an internal cache
and is not represented in config. `also.config/v1` is read once and rewritten
as v2 automatically; `path` and `source` are dropped because they are no longer
part of the model.

### State (`also.state/v1`)

Install records are tracked by opaque IDs. The ID includes the target harness
so one package can be installed into several harnesses in one directory; states
written by older versions are accepted through a legacy-ID fallback and
rewritten on the next install. Do not interpret IDs as a stable public API.

If a future change needs to alter the meaning of a stored field, `also` must
introduce `also.state/v2` plus a migration. Adding optional fields keeps v1.

### Local marker (`also.infra/v1`)

`also.json` records the package name and source harness of one initialized
directory. It is read by `also push`, `also adopt`, `also install` and `also
upgrade`. The marker is local metadata and is excluded from published
packages. Unknown fields are ignored; removing `name` or changing its meaning
needs `also.infra/v2`.

### Adapter (`also.adapter/v1`)

`schema` is optional and defaults to v1. A spec with another schema is rejected
before it is written to disk. Adapter specs are declarative; codecs and
component kinds must exist in the binary.

### Lockfile (`also.lock/v1`)

`also.lock` is project-local desired state and is meant to be committed. Each
entry records `package`, `namespace`, `remote`, `version` (content digest),
`digest` (full SHA-256), optional `constraint`, `harness`, `source_harness`,
`parts` and `with_config`. Entries are keyed by package and harness. Unknown
fields are ignored; changing the meaning of an existing field needs v2.

`--frozen` reads the lock and refuses to install anything that does not match.
A missing lock entry is an error, not an empty resolution.

### Policy (`also.policy/v1`)

`also.policy.yml` is optional. `schema` is optional and defaults to v1.
Allow lists are allowlists when non-empty; deny always wins. Package patterns
use `path.Match`. Executable booleans default to allowed. The size limit
defaults to 64 MiB. A malformed existing file is a hard error; a missing file
is not.

### Audit report (`also.audit/v1`)

`also audit --json` prints an inventory with `packages`, `findings`, `summary`
and optional `policy_file`. Findings have a severity (`fail` or `warn`) and a
stable code such as `file-missing`, `file-modified`, `block-missing`,
`block-modified`, `config-missing`, `config-modified`, `shared-file`,
`shared-block`, `shared-config`, `legacy-global`, `digest-missing`,
`policy-namespace`, `policy-package` and `policy-executable`. New codes may be
added; clients should treat an unknown code as a generic finding.

`--sbom` emits CycloneDX 1.5, not an also schema.

## Versioning policy

`also` uses semantic versioning for the CLI:

- **patch** — bug fixes, no contract change;
- **minor** — additive commands, flags, harness adapters or optional fields;
- **major** — removed commands/flags, changed defaults that alter existing
  projects, or a schema break.

Schema migrations are always forward-only and automatic for one major step.
The old form stays readable through the release that introduces its successor,
and a migration test must prove that user data survives.

## What 1.0 promises

- Existing `also.pkg/v1` packages stay installable.
- Existing `also.config/v1`/`v2` configs keep working.
- Existing `also.state/v1` installs stay upgradeable and uninstallable.
- Community adapters using `also.adapter/v1` keep loading.
- Documented commands and flags keep their meaning within a major version.

## What 1.1 adds

`also.lock`, `also.policy.yml`, `also audit`, `also drift`, `also search`,
`also tag`, semantic-version aliases and SBOM output are additive. Existing
state and packages are unchanged. `Install` gained optional `digest` and
`executables` fields inside `also.state/v1`; readers that do not know them
ignore them. `also.lock` is optional: projects without it keep resolving the
newest version, and `--frozen` is the explicit reproducibility mode.
