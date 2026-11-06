# Contributing to also

Thank you for improving `also`. This document describes the development
workflow, the quality bar and the places where a change usually belongs.

## Requirements

- Go 1.23 or newer.
- Git on the path. The test suite uses local bare repositories, so no network
  access or credentials are required.
- The real harness command-line interfaces only for `make smoke`.

## Build and test

```sh
make build     # build bin/also
make check     # gofmt, go vet, go test ./...
make smoke     # optional: exercise installed harness command-line interfaces
```

The continuous integration matrix runs `go test -count=1 -race ./...` on macOS,
Linux and Windows. Every change must keep that matrix green. The in-repository
four-by-four harness matrix in `test/e2e/matrix_test.go` runs as part of the
ordinary test suite.

## Where a change belongs

| Change | Location |
|---|---|
| Harness paths or formats | `internal/also/adapters_<harness>.go` |
| Community adapter support | `internal/also/adapter_spec.go` and `docs/ADAPTERS.md` |
| Package or version storage | `internal/also/set.go`, `internal/also/versionstore.go` |
| Installation planning or rollback | `internal/also/install.go`, `internal/also/lifecycle.go` |
| Command-line grammar | `internal/cli/grammar.go`, `internal/cli/language.go` |
| Installer experience | `internal/cli/setup.go`, `internal/cli/ui.go`, `install.sh` |
| Update behaviour | `internal/also/update.go`, `internal/cli/update.go` |

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before changing the package,
storage or rendering layers.

## Quality bar

- Run `make check` before opening a pull request.
- Add a test for every new branch, parser, security check or installer path.
  Trivial text changes do not need a new test.
- Keep user-facing text in complete English sentences. Do not shorten words for
  space. Use “namespace”, not “set”, in new user-facing code.
- Keep changes consistent with the surrounding code and with the existing
  adapter structure.
- Never add code execution to a declarative adapter.
- Do not weaken path validation, checksum verification, secret scanning or
  reversible installation.
- Update the relevant document when a contract, command or adapter changes.

## Commit and pull request style

Write a short imperative subject line, for example:

```text
fix: keep modified configuration blocks during uninstall
```

Explain the user-visible reason in the body when the change is not obvious.
Small, focused pull requests are easier to review than a mixed change.

## Adding a harness adapter

Prefer a declarative adapter when the harness reuses an existing encoding. Add
the specification, validate it with `also add harness <file>`, and document the
supported components. A new encoding still requires Go code and a registered
codec. See [docs/ADAPTERS.md](docs/ADAPTERS.md).

## Reporting security issues

Report vulnerabilities through the private channel described in
[SECURITY.md](SECURITY.md). Do not open a public issue for a vulnerability.

## License

By contributing, you agree that your contribution is released under the
Apache License 2.0 and that the project may preserve your contribution in the
NOTICE file or release history.
