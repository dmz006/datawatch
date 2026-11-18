#!/usr/bin/env bash
# Verification: the testing how-to carries the hermetic process-scan rule in the source and the embedded copy.
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"; trap 'rm -f "$CH"' EXIT
{ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u > "$CH"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|docs/plans/*) ;;
    internal/shutdowncause/*.go|docs/*.md|docs/*/*.md|internal/server/web/docs/*|internal/server/web/docs/*/*) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
for f in docs/howto/testing-against-a-running-daemon.md internal/server/web/docs/howto/testing-against-a-running-daemon.md; do
  grep -qi 'hermetic' "$f" 2>/dev/null || bad "$f lacks the word hermetic"
done
[ -f scripts/check-no-internal-refs.sh ] && { bash scripts/check-no-internal-refs.sh >/dev/null 2>&1 || bad "scripts/check-no-internal-refs.sh"; }

[ "$fail" -eq 0 ] || { echo "shutdowncause-hermetic-docs.sh: FAIL"; exit 1; }
echo "shutdowncause-hermetic-docs.sh: PASS"
