#!/usr/bin/env bash
# Verification: the generated-config test exists, passes and runs from the Makefile test target.
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
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|docs/plans/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"

[ -f tests/test-e2e-pwa-config.sh ] || bad "tests/test-e2e-pwa-config.sh is missing"
bash -n tests/test-e2e-pwa-config.sh || bad "bash -n tests/test-e2e-pwa-config.sh"
bash tests/test-e2e-pwa-config.sh || bad "tests/test-e2e-pwa-config.sh did not exit 0"
has Makefile 'bash tests/test-e2e-pwa-config\.sh'

[ "$fail" -eq 0 ] || { echo "e2e-pwa-config-test-registered.sh: FAIL"; exit 1; }
echo "e2e-pwa-config-test-registered.sh: PASS"
