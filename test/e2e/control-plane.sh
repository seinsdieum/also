#!/bin/sh
# Exercise the committed-lock workflow as an external user would run it:
# install --frozen, audit --ci, audit --sbom, then prove that policy and drift
# failures are real failures.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
cleanup() {
	chmod -R u+w "$WORK" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT HUP INT TERM

(cd "$ROOT" && go build -trimpath -o "$WORK/bin/also" .)
export HOME="$WORK/home"
export USERPROFILE="$WORK/home"
export ALSO_HOME="$WORK/also"
export ALSO_NO_UPDATE_CHECK=1
mkdir -p "$HOME" "$WORK/src" "$WORK/src2"
git init --bare -q "$WORK/remote.git"

"$WORK/bin/also" setup --yes --harness claude \
	--namespace personal="$WORK/remote.git" --default-namespace personal >/dev/null

POLICY_KEY="$WORK/policy.key"
POLICY_PUB=$("$WORK/bin/also" policy keygen --private "$POLICY_KEY" 2>/dev/null)
export ALSO_TRUSTED_POLICY_KEYS="$POLICY_PUB"

write_policy() {
	deny=${1:-}
	cat > also.policy.yml <<EOF
schema: also.policy/v1
namespaces:
  allow: [personal]
governance:
  require_signature: true
  required_approvals: 1
  required_roles: [security]
  approvers:
    - name: ci
      role: security
      key: $POLICY_PUB
EOF
	if [ -n "$deny" ]; then
		printf 'packages:\n  deny: [%s]\n' "$deny" >> also.policy.yml
	fi
	"$WORK/bin/also" policy sign --key "$POLICY_KEY" >/dev/null
}

cd "$WORK/src"
printf 'rules\n' > CLAUDE.md
printf '{"mcpServers":{"x":{"command":"/bin/echo"}}}\n' > .mcp.json
"$WORK/bin/also" init demo for claude >/dev/null
"$WORK/bin/also" push --semver 1.0.0 >/dev/null

cd "$WORK/src2"
printf 'other rules\n' > CLAUDE.md
"$WORK/bin/also" init other for claude >/dev/null
"$WORK/bin/also" push --semver 1.0.0 >/dev/null

# A user project with a committed lock and policy.
mkdir -p "$WORK/app"
cd "$WORK/app"
write_policy
git init -q .
"$WORK/bin/also" install 'demo@^1.0' for claude >/dev/null
"$WORK/bin/also" exclude infra for claude >/dev/null
"$WORK/bin/also" exclude you >/dev/null
git add .gitignore also.lock also.policy.yml also.policy.sig
git -c user.name=ci -c user.email=ci@example.invalid commit -qm fixture

"$WORK/bin/also" policy verify --ci >/dev/null
"$WORK/bin/also" audit --ci >/dev/null
"$WORK/bin/also" audit --sbom "$WORK/sbom.json" >/dev/null
grep -q 'CycloneDX' "$WORK/sbom.json"
"$WORK/bin/also" audit --policy >/dev/null

# Two packages in one project: uninstall must remove the right state record and
# the matching lock entry, not the neighbor after the slice shifts.
"$WORK/bin/also" install other for claude >/dev/null
git add also.lock
git -c user.name=ci -c user.email=ci@example.invalid commit -qm second
"$WORK/bin/also" audit --ci >/dev/null
"$WORK/bin/also" uninstall other for claude | grep -q 'other'
if grep -q '"package": "other"' also.lock; then
	echo "control-plane: uninstall left the wrong lock entry" >&2
	exit 1
fi
grep -q '"package": "demo"' also.lock
git add also.lock
git -c user.name=ci -c user.email=ci@example.invalid commit -qm removed
"$WORK/bin/also" audit --ci >/dev/null

# A second checkout must reproduce the lock exactly.
mkdir -p "$WORK/app2"
cp also.lock also.policy.yml also.policy.sig .gitignore "$WORK/app2/"
cd "$WORK/app2"
git init -q .
git add .gitignore also.lock also.policy.yml also.policy.sig
git -c user.name=ci -c user.email=ci@example.invalid commit -qm fixture
"$WORK/bin/also" install --frozen >/dev/null
"$WORK/bin/also" audit --ci >/dev/null

# Policy denial must fail the CI gate with a valid signature.
write_policy demo
if "$WORK/bin/also" audit --ci >/dev/null 2>&1; then
	echo "control-plane: policy denial did not fail audit --ci" >&2
	exit 1
fi

# Drift must fail the CI gate too.
write_policy
rm -f CLAUDE.md
if "$WORK/bin/also" audit --ci >/dev/null 2>&1; then
	echo "control-plane: missing managed block did not fail audit --ci" >&2
	exit 1
fi

echo "CONTROL PLANE OK"
