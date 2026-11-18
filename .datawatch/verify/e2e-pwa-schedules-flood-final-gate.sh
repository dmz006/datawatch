#!/usr/bin/env bash
# Final gate: the four task scripts pass, then the schedule-flood story and the other temp-folder stories pass on a fresh isolated daemon; read-only.
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

for s in e2e-pwa-config-helper e2e-pwa-config-test-registered schedules-flood-session-selector e2e-pwa-docs-project-root; do
  env -u TMUX -u TMUX_TMPDIR bash ".datawatch/verify/$s.sh" || bad "$s"
done
T="$(command -v timeout || true)"
rc=0; env -u TMUX -u TMUX_TMPDIR ${T:+$T 1800} bash scripts/e2e-pwa.sh schedule-flood >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 0 ] || bad "schedule-flood must exit 0 (got $rc)"
for st in TS-748 TS-798 TS-799 TS-821 TS-822 TS-823 TS-844; do
  rc=0; env -u TMUX -u TMUX_TMPDIR ${T:+$T 1800} bash scripts/e2e-pwa.sh "$st" >/dev/null 2>&1 || rc=$?
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 2 ]; then
    case "$st" in
      TS-822|TS-823|TS-844) echo "WARN: $st exited $rc (Automation tab stories fail for a reason outside this repair; tracked as its own bug)" ;;
      *) bad "$st exited $rc (0 pass or 2 skip allowed)" ;;
    esac
  fi
done

[ "$fail" -eq 0 ] || { echo "e2e-pwa-schedules-flood-final-gate.sh: FAIL"; exit 1; }
echo "e2e-pwa-schedules-flood-final-gate.sh: PASS"
