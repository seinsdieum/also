# Using also in continuous integration

The control-plane workflow is three commands: install exactly what the lock
pins, verify the result, and keep the evidence.

Commit all three files to the repository:

- `also.lock` — the resolved package, namespace, content version, digest,
  harness and parts;
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
      ALSO_NO_UPDATE_CHECK: "1"
      ALSO_NO_SETUP: "1"
      ALSO_TRUSTED_POLICY_KEYS: ${{ secrets.ALSO_POLICY_PUBLIC_KEYS }}
    steps:
      - uses: actions/checkout@v4

      - name: install also
        run: |
          curl -fsSL https://raw.githubusercontent.com/seinsdieum/also/main/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"

      - name: connect the namespace
        env:
          NAMESPACE_URL: ${{ secrets.ALSO_NAMESPACE_URL }}
        run: |
          also setup --yes --harness claude \
            --namespace personal="$NAMESPACE_URL" \
            --default-namespace personal

      - name: reproduce the lock
        run: also install --frozen

      - name: verify policy signatures, roles and approvals
        run: also policy verify --ci

      - name: fail on drift or a policy violation
        run: also audit --ci

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
  lock; use `also upgrade` locally and commit the new lock.
- `audit --ci` fails on a missing file, managed block or config entry, on a
  version or digest mismatch against `also.lock`, on a missing or untracked
  lock, on an install that is missing from the lock, on an unsigned or invalid
  policy signature, on a missing required role or approval, and on a policy
  violation. A user-edited file is a warning, because edits can be intentional.
  The lock is the committed source of truth; `~/.also/state.json` is only the
  local cache.
- `ALSO_TRUSTED_POLICY_KEYS` holds base64 Ed25519 public keys, comma-separated.
  The policy cannot trust its own key; see `docs/GOVERNANCE.md`.
- `audit --strict` also fails on a warning, which is the right default when the
  repository owns the files and no local edits are expected.
- `audit --sbom` writes CycloneDX 1.5 and never downloads a package.
- The namespace URL is a secret only if the repository is private. Prefer the
  platform's git credential or a read-only deploy key; `also` does not store
  tokens in its config.
- There is no policy bypass flag. A policy change is a reviewed file change.
