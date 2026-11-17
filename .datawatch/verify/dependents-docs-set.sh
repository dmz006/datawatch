#!/usr/bin/env bash
# Verification: the dependency recovery docs are written and the embedded copies match docs/.
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
allowed="internal/autonomous/dependency_recovery.go
internal/autonomous/dependency_recovery_test.go
internal/autonomous/executor.go
internal/autonomous/infra_retry_dependents.go
internal/autonomous/infra_retry_dependents_test.go
internal/autonomous/dependents_recovery_test.go
internal/autonomous/api.go
internal/autonomous/manager.go
docs/howto/autonomous-recovery.md
docs/howto/infrastructure-retry.md
docs/datawatch-definitions.md
docs/howto/README.md
docs/operations.md
internal/server/web/docs/howto/autonomous-recovery.md
internal/server/web/docs/howto/infrastructure-retry.md
internal/server/web/docs/datawatch-definitions.md
internal/server/web/docs/howto/README.md
internal/server/web/docs/operations.md
.datawatch/verify/dependents-completed-recovery.sh
.datawatch/verify/dependents-recovery-tests.sh
.datawatch/verify/dependents-docs-set.sh
.datawatch/verify/dependents-recovery-final-gate.sh"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|docs/plans/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
for d in howto/autonomous-recovery.md howto/infrastructure-retry.md datawatch-definitions.md howto/README.md operations.md; do
  [ -f "docs/$d" ] || { bad "docs/$d is missing"; continue; }
  diff -q "docs/$d" "internal/server/web/docs/$d" >/dev/null 2>&1 || bad "embedded copy differs: $d"
done
if git diff "$BASE" -- docs | grep -E '^\+' | grep -Eq '\bPRDs?\b'; then bad "added docs lines use the banned plan acronym (say Automaton)"; fi

[ "$fail" -eq 0 ] || { echo "dependents-docs-set.sh: FAIL"; exit 1; }
echo "dependents-docs-set.sh: PASS"
