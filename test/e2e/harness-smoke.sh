#!/bin/sh
# Smoke-test installed packages with the real harness CLIs, without models.
# Requires git plus whichever harness CLIs are installed; missing ones are skipped.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
cleanup() {
	chmod -R u+w "$WORK" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT HUP INT TERM

(cd "$ROOT" && mkdir -p "$WORK/bin" && go build -trimpath -o "$WORK/bin/also" .)
export HOME="$WORK/home"
export USERPROFILE="$WORK/home"
export ALSO_HOME="$WORK/also"
mkdir -p "$HOME"
git init --bare -q "$WORK/remote.git"

mkdir -p "$WORK/bundle/skills/smoke"
printf 'smoke rules\n' > "$WORK/bundle/CLAUDE.md"
printf -- '---\nname: smoke\ndescription: smoke test\n---\nbody\n' > "$WORK/bundle/skills/smoke/SKILL.md"
cat > "$WORK/bundle/.mcp.json" <<EOF
{"mcpServers":{"smoke":{"command":"/bin/echo","args":["hello"]}}}
EOF

"$WORK/bin/also" setup --yes --harness claude --namespace smoke="$WORK/remote.git" --default-namespace smoke >/dev/null
"$WORK/bin/also" push "$WORK/bundle" --name smoke >/dev/null

run_check() {
	harness=$1
	cmd=$2
	project="$WORK/$harness"
	mkdir -p "$project"
	(cd "$project" && "$WORK/bin/also" install smoke for "$harness" >/dev/null)
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "SKIP $harness (no CLI)"
		return
	fi
	case "$harness" in
	claude|qwen)
		(cd "$project" && "$cmd" mcp list 2>&1 | grep -q smoke)
		;;
	opencode)
		# debug config needs the OpenCode service; fall back to the JSON.
		if ! (cd "$project" && "$cmd" debug config 2>/dev/null | grep -q smoke); then
			python3 "$ROOT/test/e2e/check-opencode-json.py" "$project/opencode.json"
		fi
		;;
	codex)
		# Project config loads only for trusted projects; verify Codex parses
		# the generated TOML through an isolated user config instead.
		mkdir -p "$WORK/codex"
		cp "$project/.codex/config.toml" "$WORK/codex/config.toml"
		(cd "$project" && CODEX_HOME="$WORK/codex" "$cmd" mcp list 2>&1 | grep -q smoke)
		;;
	esac
	echo "OK $harness"
}

run_check claude claude
run_check opencode opencode
run_check codex codex
run_check qwen qwen
echo "HARNESS SMOKE OK"
