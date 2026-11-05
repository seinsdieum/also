# also package format (`also.pkg/v1`)

This is the interoperability contract. A package is an immutable snapshot of what the user
authored, plus metadata. It is not read by any harness directly; harnesses read the
materialized result of an install.

## Repository layout

```
also-set.json                     # {"schema":"also.set/v1","name":"personal"}
packages/<package>/<version>/
    also-pkg.json
    origin/
        ...                       # byte-for-byte bundle content
```

Rules:

- `<package>` matches `^[a-z0-9][a-z0-9._-]{0,63}$`.
- `<version>` is the first 16 hex characters of the digest (below).
- A version directory is immutable. `also` never rewrites an existing version.
- Paths inside `origin/` are slash-separated, relative, must not contain `..`, and must not be
  symlinks.

## `also-pkg.json`

```json
{
  "schema": "also.pkg/v1",
  "name": "code-review",
  "description": "Optional text",
  "source_harness": "claude",
  "label": "v1.2",
  "digest": "54883c0efc5b4330717b8fc00e5b3204431c09d51c8a5b3c1b47e7075c3bd442",
  "encoded_with": "also 0.1.0"
}
```

- `schema` — required, exactly `also.pkg/v1`.
- `name` — required, must match the directory name.
- `source_harness` — required; one of `claude`, `opencode`, `codex`, `qwen`. It selects the
  adapter used to interpret `origin/`.
- `label` — optional human alias. It is part of the digest.
- `digest` — full SHA-256 of the canonical input described below.
- `encoded_with` — informational; **not** part of the digest, so encoder upgrades do not
  create new versions.

## Digest

`version = hex(sha256(P))[:16]`, where `P` is the concatenation of these NUL-terminated fields:

```
"also.pkg/v1"
name
description
source_harness
label
```

followed, for every file in `origin/` sorted by path, by:

```
rel_path
mode ("0644" or "0755")
hex(sha256(file_bytes))
```

The digest covers exactly what the user saved plus the four metadata fields above.
It is verified on load. Two packages with identical content (including label) are the same
version.

## Origin is the source of truth

`origin/` is a complete copy of the authored bundle in its source harness layout, for example:

```
origin/CLAUDE.md
origin/skills/reviewer/SKILL.md
origin/commands/review.md
origin/agents/critic.md
origin/hooks/guard.sh
origin/settings.json
origin/.mcp.json
```

No normalization happens at encode time. This guarantees lossless round-trip for the source
harness and keeps the user's exact bytes. Normalization/translation is a pure step performed
when materializing for a target harness, and the set of supported translations is documented
in the README.

## Install state

Installs are tracked in `~/.also/state.json` (not in the package):

- every written file with its SHA-256 and mode;
- the managed instructions block (`file`, `id`, body hash, whether also created the file);
- reversible config edits (`json-set`, `json-append`, `toml-block`) with the exact value,
  the previous value, and a marker for TOML blocks.

This state is what makes `upgrade`, `reshape`, `tree`, and `uninstall` safe and reversible.

## Version resolution

- `@latest` or no suffix — version whose directory was last touched by the newest commit.
- `@<4+ hex prefix>` — unique version prefix.
- `@<label>` — newest version carrying that label.

Labels are aliases only; there is no semantic-version comparison.
