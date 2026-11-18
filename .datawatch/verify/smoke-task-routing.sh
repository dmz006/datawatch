#!/usr/bin/env bash
# Verification: the Task routing smoke section passes.
set -uo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
LOG="$(mktemp)"; trap 'rm -f "$LOG"' EXIT
rc=0
for SEC in Task; do
  DW_SKIP_VERSION_BUMP_CHECK=1 SMOKE_ONLY="$SEC" timeout 1700 bash scripts/release-smoke.sh >"$LOG" 2>&1 || rc=$?
  tail -12 "$LOG"
  if grep -Eq '^[[:space:]]*FAIL ' "$LOG"; then bad "smoke section $SEC still reports failures"; grep -E '^[[:space:]]*FAIL ' "$LOG" | head -8; fi
  [ "$rc" -eq 0 ] || bad "smoke section $SEC exited $rc"
  rc=0
done
BASE="$(git merge-base HEAD main)"
gof="$({ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u | grep '\.go$' | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
[ -z "$gof" ] || { out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
[ "$fail" -eq 0 ] || { echo "smoke-task-routing.sh: FAIL"; exit 1; }
echo "smoke-task-routing.sh: PASS"
