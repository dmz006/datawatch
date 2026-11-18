#!/usr/bin/env bash
# Verification: the session detail page has the phone-width Tags and Scheduled rollup summaries.
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

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
has internal/server/web/app.js 'session-detail-tags-rollup'
has internal/server/web/app.js 'aria-expanded'
has internal/server/web/app.js 'session_detail_tags_summary'
has internal/server/web/style.css 'session-detail-tags-rollup'
node --check internal/server/web/app.js || bad "node --check app.js"

[ "$fail" -eq 0 ] || { echo "session-detail-rollup-impl.sh: FAIL"; exit 1; }
echo "session-detail-rollup-impl.sh: PASS"
