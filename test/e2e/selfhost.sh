#!/usr/bin/env bash
# The self-hosting contract: an infrastructure repository, a product repository
# that keeps infra out of git, CI replay from the lock, harness migration and
# multi-project/multi-namespace behavior.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/also-selfhost.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

export GOMODCACHE="${GOMODCACHE:-$(go env GOMODCACHE)}"
export GOCACHE="${GOCACHE:-$(go env GOCACHE)}"
export HOME="$WORK/home"
export USERPROFILE="$WORK/home"
export ALSO_HOME="$WORK/also"
export ALSO_NO_UPDATE_CHECK=1
mkdir -p "$HOME" "$WORK/bin"
(cd "$ROOT" && go build -trimpath -o "$WORK/bin/also" .)
ALSO="$WORK/bin/also"

git init --bare -q "$WORK/infra.git"
git init --bare -q "$WORK/work.git"
"$ALSO" setup --yes --harness claude --namespace infra="$WORK/infra.git" --default-namespace infra >/dev/null
"$ALSO" connect work "$WORK/work.git" --yes >/dev/null

# An OpenCode-native team package.
INFRA="$WORK/infra"
mkdir -p "$INFRA/.opencode/skills/review" "$INFRA/.opencode/agents" "$INFRA/.opencode/commands"
printf 'team rules\n' > "$INFRA/AGENTS.md"
printf '{"default_agent":"dev","model":"opencode-go/glm-5.3","mcp":{"servers":{"x":{"type":"local","command":["/bin/echo","x"]}}}}' > "$INFRA/opencode.json"
printf -- '---\nname: review\ndescription: d\n---\nreview\n' > "$INFRA/.opencode/skills/review/SKILL.md"
printf -- '---\ndescription: dev\nmode: primary\n---\ndev\n' > "$INFRA/.opencode/agents/dev.md"
printf -- '---\ndescription: fix\n---\nfix $ARGUMENTS\n' > "$INFRA/.opencode/commands/fix.md"
(cd "$INFRA" && "$ALSO" init p for opencode >/dev/null && "$ALSO" push --semver 1.0.0 >/dev/null)
(cd "$INFRA" && "$ALSO" push --namespace work --semver 1.0.0 >/dev/null)

# A product repository: its own opencode.json, no infra in git.
P="$WORK/product"
mkdir -p "$P"
(cd "$P" && git init -q . && printf '# product\n' > README.md && printf '{"$schema":"x"}' > opencode.json && git add . && git -c user.name=dev -c user.email=dev@example.invalid commit -qm init >/dev/null)
(cd "$P" && "$ALSO" exclude infra >/dev/null)
grep -q '^\.opencode/$' "$P/.gitignore"
! grep -q '^\.claude/$' "$P/.gitignore"

# Everything except the files a product may want to own.
(cd "$P" && "$ALSO" install p --except instructions --except settings --except mcp >/dev/null)
# The project layout must select OpenCode even if the machine default is Claude.
test -e "$P/.opencode/skills/review/SKILL.md"
test ! -e "$P/AGENTS.md"
test -e "$P/.opencode/skills/review/SKILL.md"
test -e "$P/.opencode/agents/dev.md"
test -e "$P/.opencode/commands/fix.md"

# Additive selection then an explicit replacement.
(cd "$P" && "$ALSO" install agents from p for opencode >/dev/null)
test -e "$P/.opencode/skills/review/SKILL.md"
(cd "$P" && "$ALSO" install skills from p for opencode --only >/dev/null)
test ! -e "$P/.opencode/agents/dev.md"
# A plain install means the whole package again.
(cd "$P" && "$ALSO" install p for opencode >/dev/null)
test -e "$P/AGENTS.md"

# The install is pinned so a later upgrade cannot cross a major by accident.
python3 -c 'import json;d=json.load(open("'"$P"'/also.lock"));assert d["packages"][0]["constraint"]=="^1.0.0", d'

# Leaked infrastructure is a CI failure even if the lock is committed.
(cd "$P" && git add also.lock && git add -f .opencode AGENTS.md opencode.json)
if (cd "$P" && "$ALSO" audit --ci --strict >/dev/null 2>&1); then
	echo "selfhost: tracked infra did not fail audit --ci --strict" >&2
	exit 1
fi
(cd "$P" && git rm -r --cached -q .opencode AGENTS.md opencode.json && git add .gitignore also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm lock >/dev/null && "$ALSO" audit --ci --strict >/dev/null)
# Snapshot the pre-reshape, 1.0.0 checkout for the fresh-machine phase.
mkdir -p "$WORK/ci-base"
(cd "$P" && git archive HEAD | tar -x -C "$WORK/ci-base")

# A command from a subdirectory targets the project root, not a nested project.
mkdir -p "$P/cmd/app"
(cd "$P/cmd/app" && "$ALSO" audit --ci --strict >/dev/null)
test ! -e "$P/cmd/app/also.lock"

# A major release waits for --major.
V1=$(python3 -c 'import json;print(json.load(open("'"$P"'/also.lock"))["packages"][0]["version"])')
(cd "$INFRA" && printf '\n2.0\n' >> AGENTS.md && "$ALSO" push --semver 2.0.0 >/dev/null)
(cd "$P" && "$ALSO" upgrade >/dev/null)
V2=$(python3 -c 'import json;print(json.load(open("'"$P"'/also.lock"))["packages"][0]["version"])')
test "$V1" = "$V2"
(cd "$P" && "$ALSO" upgrade --major >/dev/null)
V3=$(python3 -c 'import json;print(json.load(open("'"$P"'/also.lock"))["packages"][0]["version"])')
test "$V1" != "$V3"
python3 -c 'import json;d=json.load(open("'"$P"'/also.lock"));assert d["packages"][0]["constraint"]=="^2.0.0", d'
# The next ordinary upgrade must not downgrade back into the old major.
(cd "$P" && "$ALSO" upgrade >/dev/null)
V4=$(python3 -c 'import json;print(json.load(open("'"$P"'/also.lock"))["packages"][0]["version"])')
test "$V3" = "$V4"
(cd "$P" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm major >/dev/null)

# Reshape is project-scoped and updates the lock.
Q="$WORK/product2"
mkdir -p "$Q"
(cd "$Q" && git init -q . && printf '# q\n' > README.md && git add . && git -c user.name=dev -c user.email=dev@example.invalid commit -qm init >/dev/null && "$ALSO" exclude infra for opencode >/dev/null && "$ALSO" install p for opencode >/dev/null && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm lock >/dev/null)
(cd "$P" && "$ALSO" reshape claude >/dev/null)
test -e "$P/.claude/agents/dev.md"
test ! -e "$P/.opencode/agents/dev.md"
test -e "$Q/.opencode/agents/dev.md"
python3 -c 'import json;d=json.load(open("'"$P"'/also.lock"));assert d["packages"][0]["harness"]=="claude", d'
grep -q '^\.claude/$' "$P/.gitignore"
(cd "$P" && git add -A && ! git diff --cached --name-only | grep -qE '^(\.claude/|CLAUDE\.md)' && git reset -q)
(cd "$P" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm reshape >/dev/null && "$ALSO" audit --ci --strict >/dev/null)

# A package-created harness config is removed on reshape, not left as a
# tracked artifact.
R="$WORK/product3"
mkdir -p "$R"
(cd "$R" && git init -q . && printf '# r\n' > README.md && git add . && git -c user.name=dev -c user.email=dev@example.invalid commit -qm init >/dev/null && "$ALSO" exclude infra for opencode >/dev/null && "$ALSO" install p for opencode >/dev/null && test -e opencode.json && git add .gitignore also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm lock >/dev/null)
(cd "$R" && "$ALSO" reshape claude >/dev/null)
test ! -e "$R/opencode.json"
test ! -e "$R/.opencode"
(cd "$R" && git add -A && ! git diff --cached --name-only | grep -qE '^(opencode\.json|\.opencode/|\.claude/|CLAUDE\.md)$' && git reset -q)
(cd "$R" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm reshape >/dev/null && "$ALSO" audit --ci --strict >/dev/null)

# A nested project inside a monorepo updates the git-root .gitignore.
M="$WORK/mono"
P2="$M/product"
mkdir -p "$P2"
(cd "$M" && git init -q . && printf '# mono\n' > README.md && git add . && git -c user.name=dev -c user.email=dev@example.invalid commit -qm init >/dev/null)
(cd "$P2" && "$ALSO" exclude infra for opencode >/dev/null && "$ALSO" install p for opencode >/dev/null)
(cd "$P2" && "$ALSO" reshape claude >/dev/null)
grep -q '^\.claude/$' "$M/.gitignore"
(cd "$M" && git add -A && ! git diff --cached --name-only | grep -qE '^product/(\.claude/|CLAUDE\.md)$' && git reset -q)

# The same package from two namespaces has two lock entries.
(cd "$Q" && "$ALSO" install p for opencode from work >/dev/null)
python3 -c 'import json;d=json.load(open("'"$Q"'/also.lock"));assert len(d["packages"])==2, d'
(cd "$Q" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm work >/dev/null && "$ALSO" audit --ci --strict >/dev/null)
(cd "$Q" && "$ALSO" uninstall p for opencode --namespace work >/dev/null)
python3 -c 'import json;d=json.load(open("'"$Q"'/also.lock"));assert len(d["packages"])==1, d'
(cd "$Q" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm removed >/dev/null && "$ALSO" audit --ci --strict >/dev/null)

# A fresh checkout has no state and no namespace config; the lock's remote is
# enough, and identical existing files are adopted if the tree was restored.
C="$WORK/ci"
mkdir -p "$C"
cp -a "$WORK/ci-base/." "$C/"
mkdir -p "$WORK/home-ci"
(
	export HOME="$WORK/home-ci" USERPROFILE="$WORK/home-ci" ALSO_HOME="$WORK/also-ci"
	mkdir -p "$ALSO_HOME"
	test ! -e "$ALSO_HOME/config.json"
	cd "$C"
	"$ALSO" install --frozen >/dev/null
	"$ALSO" audit --ci --strict >/dev/null
	# A real major upgrade and harness transition on a lock-only machine.
	V0=$(python3 -c 'import json;print(json.load(open("also.lock"))["packages"][0]["version"])')
	"$ALSO" upgrade --major >/dev/null
	V1=$(python3 -c 'import json;print(json.load(open("also.lock"))["packages"][0]["version"])')
	test "$V0" != "$V1"
	"$ALSO" reshape claude >/dev/null
	"$ALSO" audit --ci --strict >/dev/null
	test -e .claude/agents/dev.md
	test ! -e .opencode
	test ! -e AGENTS.md || ! grep -q 'also:begin' AGENTS.md
	test ! -e opencode.json || ! grep -q 'default_agent' opencode.json
	python3 -c 'import json;d=json.load(open("also.lock"));assert d["packages"][0]["harness"]=="claude", d'
	test "$(grep -c 'also:begin' CLAUDE.md 2>/dev/null || true)" -eq 1
	# State loss with the same files present must adopt them, not fail.
	rm -f "$ALSO_HOME/state.json"
	"$ALSO" install --frozen >/dev/null
	"$ALSO" audit --ci --strict >/dev/null
)

# A symlinked checkout is the same project, with one install slot.
ln -s "$P" "$WORK/link"
(cd "$WORK/link" && "$ALSO" audit --ci --strict >/dev/null)
COUNT=$(cd "$WORK/link" && "$ALSO" list --installs --json | python3 -c "
import json,sys,os
p=os.path.realpath('$P')
print(sum(1 for i in json.load(sys.stdin) if os.path.realpath(i['project'])==p))
")
test "$COUNT" -eq 1

# A metadata-only package must not be published accidentally.
E="$WORK/empty"
mkdir -p "$E"
(cd "$E" && git init -q . && printf '.opencode/\n' > .gitignore && "$ALSO" init empty for opencode >/dev/null)
if (cd "$E" && "$ALSO" push >/dev/null 2>&1); then
	echo "selfhost: empty package was published" >&2
	exit 1
fi
(cd "$E" && "$ALSO" push --allow-empty >/dev/null)

echo "SELF HOSTING OK"
