# Security

## Reporting

Report vulnerabilities through GitHub private security advisories on
`seinsdieum/also`. Do not open a public issue for a vulnerability.

## Trust model

Trusted:

- the `also` binary and its built-in harness adapters;
- bundles you created or reviewed and pushed yourself;
- namespaces you explicitly connected with `also connect`.

Untrusted:

- community adapters added with `also add harness <file|url>`;
- packages from namespaces you do not control;
- any URL, archive or bundle path you did not write yourself.

## Community adapters

An adapter is declarative JSON. It cannot execute code and can only select
formats and component kinds that are already registered in the binary.
Additional limits:

- `global_base` is only a detection hint and must be a directory below `$HOME`;
  all install paths are relative to the project root;
- external adapters cannot overwrite built-in ids or aliases;
- adapter specs are capped at 1 MiB;
- `also add harness <url>` warns when the download is not pinned. Pass
  `--sha256 <hash>` to verify the exact bytes before the spec is accepted;
- `also add harness` does not run the adapter: validation happens before it is
  written to `~/.also/adapters/`.

`--sha256` covers the whole spec file. It does not authenticate the author.
For reproducible installs, pin the hash in your own scripts.

## Packages and installation

Installing a package writes files into the target harness layout. `also`
checks every path, refuses symlinks, keeps reversible bookkeeping, and never
silently overwrites user changes. It does not sandbox what a skill, command,
agent or hook does when the harness later runs it. Review a package before
installing it, especially `hooks/` and executable scripts.

`also audit` records and reports the executable surface: Model Context Protocol
servers, hooks, plugins and commands. `also audit --drift` shows files, managed blocks
and configuration entries that no longer match the recorded state. Neither
command executes package content.

## Policy and continuous integration

A repository can commit `also.policy.yml` to restrict namespaces, packages,
executable component kinds, package size and literal secrets. The policy is
enforced before an install or upgrade writes anything. `also audit --ci` exits 1
on a missing file, block or config entry, on a lock/state mismatch, on an
untracked or missing `also.lock`, on an unsigned or invalid policy signature,
on a missing role or approval, on a policy violation, and on installed
infrastructure that is tracked by git; `--strict` also fails on user edits.
There is no policy bypass flag; changing the policy is a reviewed, signed file
change. The artifact-free check (`infra-tracked`, `block-tracked`,
`config-tracked`) is deliberate: infra in the product history would make the
lock and the policy review meaningless.

Policy signatures are detached Ed25519 signatures over the exact bytes of
`also.policy.yml`, stored in `also.policy.sig`. The trusted public keys live
outside the policy (`~/.also/trusted-policy-keys` or
`ALSO_TRUSTED_POLICY_KEYS`), so a policy cannot trust its own key. Signatures
prove that a trusted approver reviewed those bytes; they do not sandbox what a
hook or MCP server does later.

`also.lock` is the reproducibility control. `also install --frozen` fails
instead of resolving a version or digest that differs from the lock. Commit the
lockfile and review changes to it like any other dependency lock.

## Secrets

`also push` refuses to encode literal credential-looking keys in MCP settings,
JSON/YAML/TOML config files, plain-text notes, command arguments and URLs.
Environment references (`$TOKEN`, `${TOKEN}`, `{env:TOKEN}`) are allowed because
the value is not stored in the package.

`--allow-secrets` bypasses the check and should be used only for intentional
fixtures. Error messages list key locations, not secret values.

## Namespace transport

Namespaces use the system `git` and its credential helper/SSH keys. `also`
does not store tokens in its config. Clones use `GIT_TERMINAL_PROMPT=0`, so a
missing credential fails instead of hanging.

## Verifying a release

Release binaries are built in GitHub Actions and get a Sigstore build
provenance attestation. After downloading a binary or `SHA256SUMS`:

```sh
gh attestation verify also-darwin-arm64 --repo seinsdieum/also
sha256sum -c SHA256SUMS --ignore-missing
```

`install.sh` and `install.ps1` verify the SHA-256 checksum and, when the GitHub
CLI is installed and authenticated, run `gh attestation verify` before the
binary is installed. The installer does the same for its download. The
attestation binds that binary to the release workflow and commit; it does not
audit the source for you.

Every release also publishes `also.spdx.json`, an SPDX SBOM generated from the
release artifacts, and `also audit --sbom` emits a CycloneDX 1.5 inventory of
what is installed. These documents are evidence, not a sandbox.
