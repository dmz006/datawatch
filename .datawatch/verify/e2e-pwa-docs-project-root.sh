#!/usr/bin/env bash
# Verification: the PWA e2e docs mention the temp project root, and the embedded copies match.
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
allowed="scripts/e2e-pwa-config.sh
scripts/e2e-pwa.sh
tests/test-e2e-pwa-config.sh
Makefile
scripts/test-stories/pwa/TS-797.mjs
docs/testing/README.md
docs/howto/navigation-mobile-fit.md
docs/testing-tracker.md
internal/server/web/docs/testing-tracker.md
internal/server/web/docs/testing/README.md
internal/server/web/docs/howto/navigation-mobile-fit.md
.datawatch/verify/e2e-pwa-config-helper.sh
.datawatch/verify/e2e-pwa-config-test-registered.sh
.datawatch/verify/schedules-flood-session-selector.sh
.datawatch/verify/e2e-pwa-docs-project-root.sh
.datawatch/verify/e2e-pwa-schedules-flood-final-gate.sh"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|docs/plans/*|scripts/test-stories/pwa/node_modules|scripts/test-stories/pwa/node_modules/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
has docs/testing/README.md 'allowed_project_roots'
has docs/testing/README.md 'project root'
has docs/howto/navigation-mobile-fit.md 'project root'
for d in testing/README.md howto/navigation-mobile-fit.md; do
  diff -q "docs/$d" "internal/server/web/docs/$d" >/dev/null 2>&1 || bad "embedded copy differs: $d"
done

[ "$fail" -eq 0 ] || { echo "e2e-pwa-docs-project-root.sh: FAIL"; exit 1; }
echo "e2e-pwa-docs-project-root.sh: PASS"
