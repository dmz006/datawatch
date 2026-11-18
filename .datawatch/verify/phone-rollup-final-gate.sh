#!/usr/bin/env bash
# Final gate: the four Automaton scripts pass and the PWA node suite and locale guard pass; read-only.
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
has() { grep -Eq -- "$2" "$1" 2>/dev/null || bad "$1 lacks: $2"; }
BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"; trap 'rm -f "$CH"' EXIT
{ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u > "$CH"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|AGENT.md|CLAUDE.md|docs/plans/*) ;;
    internal/server/web/app.js|internal/server/web/style.css|internal/server/web/locales/*.json|internal/server/v5280_locales_test.go|internal/server/web/app-session-detail-rollup.test.js|docs/howto/sessions-deep-dive.md|internal/server/web/docs/howto/sessions-deep-dive.md) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"

for s in session-detail-rollup-impl session-detail-rollup-locales session-detail-rollup-tests session-detail-rollup-docs; do
  env -u TMUX -u TMUX_TMPDIR bash ".datawatch/verify/$s.sh" || bad "$s"
done
node --test internal/server/web/*.test.js >/dev/null 2>&1 || bad "node --test internal/server/web/*.test.js"
go test ./internal/server/ -count=1 -run 'TestLocales' >/dev/null 2>&1 || bad "locale tests"

[ "$fail" -eq 0 ] || { echo "phone-rollup-final-gate.sh: FAIL"; exit 1; }
echo "phone-rollup-final-gate.sh: PASS"
