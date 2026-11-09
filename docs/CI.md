# Using also in continuous integration

The control-plane workflow is three commands: install exactly what the lock
pins, verify the result, and keep the evidence.

Commit these files to the repository:

- `.gitignore` — the managed infra/also blocks from `also exclude`;
- `also.lock` — the resolved package, namespace, content version, digest,
  harness, parts and exclusions;
- `also.policy.yml` — allowed namespaces and packages, executable gates,
  limits and the governance block;
- `also.policy.sig` — the detached Ed25519 approvals over the policy bytes.

A minimal GitHub Actions job:

```yaml
name: agent-config
on: [pull_request]

jobs:
  verify:
    runs-on: ubuntu-latest
    env:
      ALSO_NO_SETUP: "1"
      ALSO_TRUSTED_POLICY_KEYS: ${{ secrets.ALSO_POLICY_PUBLIC_KEYS }}
    steps:
      - uses: actions/checkout@v4

      - name: install also
        run: |
          curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"

      - name: reproduce the lock
        run: also install --frozen --require-policy

      - name: fail on drift, tracked infrastructure or a policy violation
        run: also audit --ci --require-policy

      - name: fail on any local edit
        run: also audit --strict

      - name: upload the SBOM
        if: always()
        run: also audit --sbom also.cdx.json

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: also-sbom
          path: also.cdx.json
```

Notes:

- `install --frozen` fails if the lock is missing, if a package resolves to a
  different version, or if a digest differs. It never silently updates the
  lock; use `also upgrade` locally and commit the new lock. The lock records
  the remote, so a fresh checkout needs no namespace setup.
- `--require-policy` closes the gap that `--frozen` and `audit --ci` leave
  open: `also.policy.yml` is optional by default, so deleting it would
  otherwise remove the gate without failing the job. With the flag, a missing
  policy exits 1 before anything is written. Run it on both steps so neither
  can proceed without a policy. The flag only proves that a policy exists; the
  signature, role and content checks are the ones below.
- `audit --ci` fails on a missing file, managed block or config entry, on a
  version or digest mismatch against `also.lock`, on a missing or untracked
  lock, on an install that is missing from the lock, on an unsigned or invalid
  policy signature, on a missing required role or approval, on a policy
  violation, and on installed infrastructure that is tracked by git
  (`infra-tracked`, `block-tracked`, `config-tracked`, `stale-infra`). A
  user-edited file is a warning, because edits can be intentional. The lock is
  the committed source of truth; `~/.also/state.json` is only the local cache.
- The artifact-free setup is: `also exclude infra for <harness>` and
  `also exclude you` before the first install, then commit only `.gitignore`,
  `also.lock`, `also.policy.yml` and `also.policy.sig`. The audit check above
  catches a tracked `.opencode/`, `AGENTS.md`, `CLAUDE.md` or harness config.
  For a self-hosting gate use `audit --ci --strict`; `--ci` alone treats a
  modified generated file as a warning, while `--strict` catches package
  collisions and local drift.
- `ALSO_TRUSTED_POLICY_KEYS` holds base64 Ed25519 public keys, comma-separated.
  The policy cannot trust its own key; see `docs/GOVERNANCE.md`.
- `audit --strict` also fails on a warning, which is the right default when the
  repository owns the files and no local edits are expected.
- `audit --sbom` writes CycloneDX 1.5 and never downloads a package.
- The namespace URL is a secret only if the repository is private. Prefer the
  platform's git credential or a read-only deploy key; `also` does not store
  tokens in its config.
- There is no flag that bypasses the rules of an existing policy. A policy
  change is a reviewed, signed file change; keep `--require-policy` in CI so
  deleting the file cannot quietly remove the gate.
