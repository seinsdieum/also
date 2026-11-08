# Releasing

A release is a tag plus published artifacts. The only question that matters is
whether the exact commit being tagged has passed the full gate and the
released-binary E2E.

## Before the tag

1. `make release-check` passes locally. It runs formatting, vet, the full test
   suite, the race detector, `go mod verify` and the control-plane E2E.
2. `make smoke` passes on a machine with the real harness command-line
   interfaces. It never calls a model.
3. `CHANGELOG.md` has a section for the version and the version string in
   `internal/also/paths.go` matches it.
4. `docs/CLI.md`, `docs/CONTRACTS.md`, `docs/GOVERNANCE.md` and `README.md`
   describe the shipped behavior, including lock, policy and signature fields.
5. CI on `main` is green for the exact commit that will be tagged.

If a check is red, the release does not exist. There is no alternate gate and no
tagging around a failure.

## Tagging

- Tag the commit that CI verified. Do not tag `HEAD` if `HEAD` moved after CI.
- Use an annotated tag: `git tag -a vX.Y.Z -m "..." <commit>`.
- Do not move or delete a published tag unless the release is broken and the
  replacement is documented in `CHANGELOG.md`.

## After the tag

1. The release workflow runs the full gate, builds all six binaries, generates
   `also.spdx.json`, signs build provenance and publishes the release.
2. Verify the published artifacts:
   - `gh release view vX.Y.Z` lists seven assets plus `SHA256SUMS`;
   - `gh attestation verify <binary> -R seinsdieum/also` succeeds;
   - `install.sh` installs the expected version and commit;
   - a released-binary E2E runs `init`, `push --semver`, `install --frozen`,
     `policy verify --ci`, `audit --ci` and `audit --sbom`.
3. Add the release URL to the changelog if it is not already there.

## Version policy

- **patch** — bug fixes and documentation, no contract change;
- **minor** — new commands, flags, adapters or optional contract fields;
- **major** — removed commands/flags, changed defaults that alter existing
  projects, or a schema break.

The release workflow enforces the same checks as CI, including `-race`; a tag
does not get a weaker gate than a pull request.
