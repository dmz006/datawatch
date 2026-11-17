#!/usr/bin/env bash
# Verification: failed dependents of a task that completes are requeued (implementation files, build, existing infra-chain tests).
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
internal/autonomous/executor.go
internal/autonomous/infra_retry_dependents.go
.datawatch/verify/dependents-completed-recovery.sh"
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

[ -f internal/autonomous/dependency_recovery.go ] || bad "internal/autonomous/dependency_recovery.go is missing"
go build ./internal/autonomous/ || bad "go build"
go test ./internal/autonomous/ -count=1 -run 'InfraRetry|Dependen' || bad "existing infra-chain and dependency tests"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "dependents-completed-recovery.sh: FAIL"; exit 1; }
echo "dependents-completed-recovery.sh: PASS"
