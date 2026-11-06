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

- `global_base` must be a directory below `$HOME`, and every global path must
  be relative to it;
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

## Secrets

`also push` refuses to encode literal credential-looking keys in MCP settings,
JSON/YAML/TOML config files, command arguments and URLs. Environment references
(`$TOKEN`, `${TOKEN}`, `{env:TOKEN}`) are allowed because the value is not
stored in the package.

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

`install.sh` verifies the SHA-256 checksum before installing. The attestation
binds that binary to the release workflow and commit; it does not audit the
source for you.
