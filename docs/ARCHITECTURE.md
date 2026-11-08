# Architecture

`also` has four core layers plus two project-level contracts. Keeping them
separate is what allows a new harness to reuse an existing encoding without
touching the installer, and lets audit inspect an install without downloading
the package again.

## 1. Package format

A package is a directory with `also-pkg.json` and an `origin/` tree. The
manifest records the name, source harness, label, digest and creation time. The
digest covers the manifest fields and every file path, mode and byte. See
[PACKAGE_FORMAT.md](PACKAGE_FORMAT.md).

A package is harness-neutral. `origin/` stores files using the bundle layout of
the source harness, and `.also-layers/` preserves native data that cannot be
expressed in another harness.

## 2. Namespace storage

A namespace is a remote git repository. The local clone is an internal cache
under `~/.also/cache/`.

```text
packages/<package>/also-pkg.json        live manifest
packages/<package>/origin/...           live portable tree
refs/tags/also/<package>/<version>      immutable versions
```

Pushing writes the live tree, commits it and creates a version tag. Reading an
old version uses `git archive` against the tag. A version tag is accepted only
when the manifest digest in that commit matches the tag name.

## 3. Adapters and rendering

An adapter declares paths, component directories, instruction file names,
configuration formats and detection rules. The built-in adapters live in
`adapters_<harness>.go`; community adapters are declarative JSON files loaded
from `~/.also/adapters/` or `ALSO_ADAPTERS`.

Rendering has three stages:

1. classify package files into instructions, skills, commands, agents, plugins,
   hooks, configuration and other files;
2. translate Model Context Protocol servers, hooks and settings through the
   registered codecs;
3. write files, managed instruction blocks and reversible configuration edits.

The plan is computed before anything is written. Applying a plan is
transactional: a failure restores the previous files and state.

## 4. Installation state

`~/.also/state.json` records every installation slot, created file, managed
block and configuration edit. An installation slot includes the namespace,
package, harness and directory, so one package can be installed into several
harnesses in the same directory.

Uninstalling consults that state. Files changed by the user are left in place
unless `--force` is used. Configuration edits are removed only when the current
value still matches what `also` wrote. Resources shared by another installation
are detected and kept.

## 5. Project contracts

Two files live next to the project rather than in `~/.also`:

- `also.lock` pins the resolved package, namespace, content version, digest,
  harness and parts. It is the reproducible input for `install --frozen`.
- `also.policy.yml` restricts namespaces, packages, executable component kinds,
  package size and literal secrets. It is checked before writes and re-checked
  by `audit --ci`.

Both are optional. Neither replaces `state.json`: the lock says what should be
installed, the state says what was actually written on this machine.

## 6. Audit and evidence

`also audit` compares the state record with the file system: files by SHA-256,
managed blocks by body hash, JSON edits by path and value, TOML blocks by
marker and body. It also reports the executable surface recorded at install
time and emits CycloneDX 1.5 through `--sbom`. Audit never trusts the package
cache and never re-downloads a package.

## Extension points

- **Adapters** add a harness.
- **Model Context Protocol codecs** add a configuration format.
- **Command, agent, hook and settings converters** add translation rules.
- **Component kinds** add a new portable directory type.

Adapters and external specifications cannot execute code. All paths are
validated, symbolic links are rejected, and install destinations stay inside
the project root. `global_base` is only a detection hint for `also setup`.

## Related documents

- [CLI.md](CLI.md)
- [ADAPTERS.md](ADAPTERS.md)
- [CONTRACTS.md](CONTRACTS.md)
- [PACKAGE_FORMAT.md](PACKAGE_FORMAT.md)
