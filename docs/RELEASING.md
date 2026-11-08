# Releasing

A release is a tag plus a published artifact. Treat the tag as the point where
the project asks strangers to trust the binary. The rules below exist because a
feature-complete release is not the same thing as a checked release.

## Before the tag

1. `make release-check` passes locally. It runs formatting, vet, the full test
   suite, the race detector, `go mod verify` and the control-plane E2E.
2. `make smoke` passes on a machine with the real harness command-line
   interfaces. It never calls a model.
3. `CHANGELOG.md` has a section for the version and the version string in
   `internal/also/paths.go` matches it.
4. `docs/CLI.md`, `docs/CONTRACTS.md` and `README.md` describe the shipped
   behavior, including any new lock, policy or audit field.
5. CI on `main` is green for the exact commit that will be tagged.

## Tagging

- Feature releases wait at least 24 hours after the last functional change on
  `main`. Hotfixes may ship sooner, but only after the full gate and a manual
  E2E with the built binary.
- Tag the commit that CI verified. Do not tag `HEAD` if `HEAD` moved after CI.
- Use an annotated tag: `git tag -a vX.Y.Z -m "..." <commit>`.
- Do not move or delete a published tag unless the release is broken and the
  replacement is documented in `CHANGELOG.md`.

## After the tag

1. The release workflow runs `test`, builds all six binaries, generates
   `also.spdx.json`, signs build provenance and publishes the release.
2. Verify the published artifacts:
   - `gh release view vX.Y.Z` lists seven assets plus `SHA256SUMS`;
   - `gh attestation verify <binary> -R seinsdieum/also` succeeds;
   - `install.sh` installs the expected version and commit;
   - a released-binary E2E runs `init`, `push --semver`, `install --frozen`,
     `audit --ci` and `audit --sbom`.
3. Add the release URL to the changelog if it is not already there.

## Version policy

- **patch** — bug fixes and documentation, no contract change;
- **minor** — new commands, flags, adapters or optional contract fields;
- **major** — removed commands/flags, changed defaults that alter existing
  projects, or a schema break.

The release workflow enforces the same checks as CI, including `-race`; a tag
does not get a weaker gate than a pull request.
