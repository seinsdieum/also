# Policy governance

`also.policy.yml` is enforced by the same binary that installs packages, so the
file needs an independent trust anchor. `also` uses detached Ed25519 signatures
over the exact committed policy bytes and a local trusted-key list.

The policy can name approvers and roles, but a key is trusted only when it is
configured outside the policy:

- `~/.also/trusted-policy-keys`, managed with `also policy trust`;
- `ALSO_TRUSTED_POLICY_KEYS`, a comma-separated list of base64 public keys for
  CI.

A policy cannot make itself trusted by naming its own key. An attacker who
changes the policy, the approver list or the signatures still cannot produce a
valid signature from a key the verifier trusts.

## Signed policy

```yaml
schema: also.policy/v1
namespaces:
  allow: [personal, work]
  deny: [untrusted]
packages:
  deny: [experiment, "scratch/*"]
executables:
  allow_hooks: true
  allow_mcp: true
  allow_plugins: false
  allow_commands: true
limits:
  max_package_bytes: 67108864
  allow_literal_secrets: false
governance:
  require_signature: true
  required_approvals: 2
  required_roles: [security, maintainer]
  approvers:
    - name: alice
      role: security
      key: <base64 Ed25519 public key>
    - name: bob
      role: maintainer
      key: <base64 Ed25519 public key>
```

`also.policy.sig` is committed next to the policy:

```json
{
  "schema": "also.policy.sig/v1",
  "signatures": [
    {
      "key": "<base64 public key>",
      "signature": "<base64 signature>",
      "signed_at": "2026-11-08T00:00:00Z"
    }
  ]
}
```

The signature covers the exact bytes of `also.policy.yml`. Any edit invalidates
it, including whitespace; re-sign after an approved change.

## Workflow

Create a key once:

```sh
also policy keygen --private ~/.also/keys/policy-alice.key --public ~/.also/keys/policy-alice.pub
```

Put the public key into `governance.approvers`, trust it locally, and sign:

```sh
also policy trust --key "$(cat ~/.also/keys/policy-alice.pub)" --name alice
also policy sign --key ~/.also/keys/policy-alice.key
also policy verify --ci
```

The second approver repeats `also policy sign` with their own key; the signature
file keeps both entries.

In CI, provide the trusted public keys without writing a home directory file:

```yaml
env:
  ALSO_TRUSTED_POLICY_KEYS: ${{ secrets.ALSO_POLICY_PUBLIC_KEYS }}
```

Then:

```sh
also policy verify --ci
also audit --ci
```

`audit --ci` fails when a policy exists but is unsigned, when a signature is
invalid or untrusted, when a required role has no signature, and when fewer
approvals exist than `required_approvals`. A signed policy that verifies still
has to pass the namespace, package and executable rules.

## Roles and approvals

- `required_approvals` is the number of distinct trusted approver keys that must
  sign the current policy. It defaults to 1.
- `required_roles` lists roles that must each have at least one valid signature.
- `approvers` maps a key to a display name and role. Duplicate signatures from
  one key count once.

## Rotation

Add the new public key to `approvers`, sign with the new private key, verify,
then remove the old key and re-sign. Removing a key from the policy invalidates
the old signature file, so the change must go through the same review as any
other policy edit.

## What this does not replace

Signatures prove that a trusted approver signed the policy bytes. They do not
prove that the namespace packages themselves are safe, and they do not sandbox
hooks or MCP servers. Use branch protection and review for who may merge the
policy, and use `audit`, `drift`, SBOM and package digests for what is actually
installed.
