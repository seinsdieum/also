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
| Policy signature | `also.policy.sig/v1` | stable |
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
and the canonical project path; symlinked paths are the same project. States
written by older versions are accepted through a legacy-ID fallback and
rewritten on the next install. Do not interpret IDs as a stable public API.
An install records `parts` and `exclude`; config edits carry a `base` value so
the last uninstall restores the user's original value regardless of order.

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
`digest` (full SHA-256), optional `semver`, `constraint`, `harness`,
`source_harness`, `parts`, `exclude` and `with_config`. Entries are keyed by
namespace, package and harness, so the same package from two namespaces can
coexist. `semver` is the semantic alias used for the installed content and lets
`audit` reject a lock whose `constraint` no longer matches its version. Unknown
fields are ignored; changing the meaning of an existing field needs v2.

`--frozen` reads the lock and refuses to install anything that does not match.
A missing lock entry is an error, not an empty resolution. If the namespace is
not configured, the `remote` in the entry is used; this is what makes a fresh
CI checkout work without a separate setup step.

### Policy (`also.policy/v1`)

`also.policy.yml` is optional. `schema` is optional and defaults to v1.
Allow lists are allowlists when non-empty; deny always wins. Package patterns
use `path.Match`. Executable booleans default to allowed. The size limit
defaults to 64 MiB. A malformed existing file is a hard error; a missing file
is not.

`governance` is optional and has `require_signature`, `required_approvals`,
`required_roles` and `approvers` (name, role, base64 Ed25519 public key). The
signature file is `also.policy.sig`, schema `also.policy.sig/v1`, with an array
of `key`, `signature` and `signed_at`. Signatures cover the exact bytes of the
policy file. Trusted public keys are external to the policy
(`~/.also/trusted-policy-keys` or `ALSO_TRUSTED_POLICY_KEYS`).

`also audit --ci` treats an unsigned policy, an invalid or untrusted signature,
a missing required role and insufficient approvals as failures. A legacy policy
without `governance` still installs locally but fails the CI gate.

### Audit report (`also.audit/v1`)

`also audit --json` prints an inventory with `packages`, `findings`, `summary`,
`lock` (file, exists, tracked, entries, verified) and optional `policy_file`.
Findings have a severity (`fail`, `warn` or `info`; `info` marks shared
identical resources and does not fail `--strict`) and a stable code such as
`file-missing`, `file-modified`, `block-missing`,
`block-modified`, `config-missing`, `config-modified`, `shared-file`,
`shared-block`, `shared-config`, `legacy-global`, `digest-missing`,
`policy-namespace`, `policy-package`, `policy-executable`, `policy-unsigned`,
`policy-signature-invalid`, `infra-tracked`, `block-tracked`,
`config-tracked`, `stale-infra` and `lock-constraint-mismatch`. New codes may be
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

## Pre-1.0 contract

The project is at `0.x`. Schema identifiers are versioned independently of the
binary and stay readable:

- `also.pkg/v1` packages stay installable.
- `also.config/v1`/`v2` configs keep working.
- `also.state/v1` installs stay upgradeable and uninstallable.
- `also.lock/v1` and `also.policy/v1` keep their documented fields.
- Community adapters using `also.adapter/v1` keep loading.

The command surface itself may change in a minor release until `1.0.0`. Within
one release, documented flags and JSON shapes keep their meaning. `0.1.0`
deliberately folds the read path into `audit` and `list`, keeps four core
commands, and hides advanced commands from `also --help`; there is no
compatibility promise for the temporary `1.x` tags that preceded this
renumbering.

`also.policy.sig` is a detached Ed25519 signature over the exact policy bytes.
`audit --ci` fails when a policy is unsigned, when a signature, role or approval
is missing, and when a package or executable violates the policy. The trust
anchor lives outside the policy file.

Every policy from the project directory up to the git root is enforced. Allow
lists intersect, deny lists and required roles union, executable gates take the
stricter value, and the smallest size limit wins. A nested policy can add
restrictions but cannot loosen the root policy.
