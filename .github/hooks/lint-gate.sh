#!/usr/bin/env bash
# PostToolUse hook: run the dayzctl lint gate after any edit that touches code
# and block the agent when it fails. Skills are advisory; this is the part that
# cannot be forgotten.
#
# Input: hook JSON on stdin (tool_name / tool_input with the edited paths).
# Output: nothing when green; {"decision":"block","reason":...} when red.
set -uo pipefail

mode="${1:---fast}"
case "$mode" in
--fast | --full) ;;
*) mode="--fast" ;;
esac

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)" || exit 0
cd "$root" || exit 0

gitdir="$(git rev-parse --git-dir 2>/dev/null || printf '/tmp')"
marker="$gitdir/lint-gate.pending"

payload="$(cat)"

# The Stop hook must not loop: the host sets stop_hook_active when the session
# is already continuing because a stop hook blocked it.
if [ "$mode" = "--full" ] && printf '%s' "$payload" | grep -q '"stop_hook_active"[[:space:]]*:[[:space:]]*true'; then
	exit 0
fi

targets="$(printf '%s' "$payload" | python3 -c '
import json, sys

try:
    data = json.load(sys.stdin)
except Exception:
    print("__unknown__")
    raise SystemExit(0)

exts = (".go", ".js", ".css", ".html")

def walk(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, dict):
        for item in value.values():
            yield from walk(item)
    elif isinstance(value, list):
        for item in value:
            yield from walk(item)

tool_input = data.get("tool_input")
if not isinstance(tool_input, (dict, list)):
    # Older/unknown payload: fall back to "did the working tree change?".
    print("__unknown__")
    raise SystemExit(0)

print(" ".join(p for p in walk(tool_input) if p.endswith(exts)))
' 2>/dev/null)"

if [ "$mode" = "--full" ]; then
	# Session end: run when a fast gate marked a code change, or when the
	# working tree still holds modified code (edits made outside the file
	# tools, e.g. a shell command).
	if [ ! -e "$marker" ]; then
		dirty="$(git status --porcelain 2>/dev/null | awk '{print $NF}' | grep -E '\.(go|js|css|html)$' | head -n 1 || true)"
		[ -n "$dirty" ] || exit 0
	fi
fi

if [ "$mode" = "--fast" ]; then
	if [ "$targets" = "__unknown__" ]; then
		# Only when the hook payload carries no tool_input at all.
		targets="$(git status --porcelain 2>/dev/null | awk '{print $NF}' | grep -E '\.(go|js|css|html)$' | head -n 1 || true)"
	fi
	[ -z "$targets" ] && exit 0
fi

fail=""
check() {
	local label="$1"
	shift
	local out code
	out="$("$@" 2>&1)"
	code=$?
	if [ "$code" -ne 0 ]; then
		fail+="[$label] exit $code"$'\n'"$out"$'\n'
	fi
}

check gofmt bash -c 'f=$(gofmt -l internal cmd tools test); [ -z "$f" ] || { printf "%s\n" "$f"; exit 1; }'
check vet go vet ./...
check build go build ./...
if [ "$mode" = "--full" ]; then
	check test go test ./... -count=1
fi
check js bash -c 'for f in web/log-analyzer/js/*.js; do cp "$f" "/tmp/lintgate-$(basename "$f").mjs" || exit 1; node --check "/tmp/lintgate-$(basename "$f").mjs" || exit 1; done'

if [ -z "$fail" ]; then
	if [ "$mode" = "--fast" ]; then
		: >"$marker"
	else
		rm -f "$marker"
	fi
	exit 0
fi

# Keep the marker so the Stop gate still runs the tests for this change.
: >"$marker"

python3 -c '
import json, sys
reason = "Lint gate failed. Fix these before continuing:\n\n" + sys.argv[1][:6000]
print(json.dumps({
    "decision": "block",
    "reason": reason,
    "systemMessage": "Lint gate FAILED (gofmt/vet/build/test/node) — see the blocked tool result.",
}))
' "$fail"
exit 0
