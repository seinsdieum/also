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
mkdir -p "$HOME" "$WORK/src"
git init --bare -q "$WORK/remote.git"

"$WORK/bin/also" setup --yes --harness claude \
	--namespace personal="$WORK/remote.git" --default-namespace personal >/dev/null

cd "$WORK/src"
printf 'rules\n' > CLAUDE.md
printf '{"mcpServers":{"x":{"command":"/bin/echo"}}}\n' > .mcp.json
"$WORK/bin/also" init demo for claude >/dev/null
"$WORK/bin/also" push --semver 1.0.0 >/dev/null

# A user project with a committed lock and policy.
mkdir -p "$WORK/app"
cd "$WORK/app"
printf 'schema: also.policy/v1\nnamespaces:\n  allow: [personal]\n' > also.policy.yml
"$WORK/bin/also" install 'demo@^1.0' for claude >/dev/null
git init -q .
git add also.lock also.policy.yml CLAUDE.md
git -c user.name=ci -c user.email=ci@example.invalid commit -qm fixture

"$WORK/bin/also" audit --ci >/dev/null
"$WORK/bin/also" audit --sbom "$WORK/sbom.json" >/dev/null
grep -q 'CycloneDX' "$WORK/sbom.json"
"$WORK/bin/also" policy >/dev/null

# A second checkout must reproduce the lock exactly.
mkdir -p "$WORK/app2"
cp also.lock also.policy.yml "$WORK/app2/"
cd "$WORK/app2"
git init -q .
git add also.lock also.policy.yml
git -c user.name=ci -c user.email=ci@example.invalid commit -qm fixture
"$WORK/bin/also" install --frozen >/dev/null
"$WORK/bin/also" audit --ci >/dev/null

# Policy denial must fail the CI gate.
printf 'schema: also.policy/v1\npackages:\n  deny: [demo]\n' > also.policy.yml
if "$WORK/bin/also" audit --ci >/dev/null 2>&1; then
	echo "control-plane: policy denial did not fail audit --ci" >&2
	exit 1
fi

# Drift must fail the CI gate too.
printf 'schema: also.policy/v1\nnamespaces:\n  allow: [personal]\n' > also.policy.yml
rm -f CLAUDE.md
if "$WORK/bin/also" audit --ci >/dev/null 2>&1; then
	echo "control-plane: missing managed block did not fail audit --ci" >&2
	exit 1
fi

echo "CONTROL PLANE OK"
