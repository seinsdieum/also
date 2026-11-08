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
(cd "$P" && "$ALSO" install p for opencode --except instructions --except settings --except mcp >/dev/null)
test ! -e "$P/AGENTS.md"
test -e "$P/.opencode/skills/review/SKILL.md"
test -e "$P/.opencode/agents/dev.md"
test -e "$P/.opencode/commands/fix.md"

# Additive selection then an explicit replacement.
(cd "$P" && "$ALSO" install agents from p for opencode >/dev/null)
test -e "$P/.opencode/skills/review/SKILL.md"
(cd "$P" && "$ALSO" install skills from p for opencode --only >/dev/null)
test ! -e "$P/.opencode/agents/dev.md"
(cd "$P" && "$ALSO" install p for opencode --only >/dev/null)
test -e "$P/AGENTS.md"

# The install is pinned so a later upgrade cannot cross a major by accident.
python3 -c 'import json;d=json.load(open("'"$P"'/also.lock"));assert d["packages"][0]["constraint"]=="^1.0.0", d'

# Leaked infrastructure is a CI failure even if the lock is committed.
(cd "$P" && git add also.lock && git add -f .opencode AGENTS.md opencode.json)
if (cd "$P" && "$ALSO" audit --ci >/dev/null 2>&1); then
	echo "selfhost: tracked infra did not fail audit --ci" >&2
	exit 1
fi
(cd "$P" && git rm -r --cached -q .opencode AGENTS.md opencode.json && git add .gitignore also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm lock >/dev/null && "$ALSO" audit --ci >/dev/null)

# A command from a subdirectory targets the project root, not a nested project.
mkdir -p "$P/cmd/app"
(cd "$P/cmd/app" && "$ALSO" audit --ci >/dev/null)
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
(cd "$P" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm reshape >/dev/null && "$ALSO" audit --ci >/dev/null)

# The same package from two namespaces has two lock entries.
(cd "$Q" && "$ALSO" install p for opencode from work >/dev/null)
python3 -c 'import json;d=json.load(open("'"$Q"'/also.lock"));assert len(d["packages"])==2, d'
(cd "$Q" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm work >/dev/null && "$ALSO" audit --ci >/dev/null)
(cd "$Q" && "$ALSO" uninstall p for opencode --namespace work >/dev/null)
python3 -c 'import json;d=json.load(open("'"$Q"'/also.lock"));assert len(d["packages"])==1, d'
(cd "$Q" && git add also.lock && git -c user.name=dev -c user.email=dev@example.invalid commit -qm removed >/dev/null && "$ALSO" audit --ci >/dev/null)

# A fresh checkout has no state and no namespace config; the lock's remote is
# enough, and identical existing files are adopted if the tree was restored.
C="$WORK/ci"
mkdir -p "$C"
(cd "$P" && git archive HEAD | tar -x -C "$C")
mkdir -p "$WORK/home-ci"
(
	export HOME="$WORK/home-ci" USERPROFILE="$WORK/home-ci" ALSO_HOME="$WORK/also-ci"
	mkdir -p "$ALSO_HOME"
	cp "$WORK/also/config.json" "$ALSO_HOME/config.json" 2>/dev/null || true
	rm -f "$ALSO_HOME/state.json"
	cd "$C"
	"$ALSO" install --frozen >/dev/null
	"$ALSO" audit --ci >/dev/null
	# State loss with the same files present must adopt them, not fail.
	rm -f "$ALSO_HOME/state.json"
	"$ALSO" install --frozen >/dev/null
	"$ALSO" audit --ci >/dev/null
)

# A symlinked checkout is the same project, with one install slot.
ln -s "$P" "$WORK/link"
(cd "$WORK/link" && "$ALSO" audit --ci >/dev/null)
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
