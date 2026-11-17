#!/usr/bin/env bash
# Verification: the pre-commit security scan is wired, warn-only, and recorded on the task.
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
allowed="internal/session/manager.go
internal/session/manager_precommit_scan_test.go
internal/autonomous/security_precommit.go
internal/autonomous/models.go
internal/autonomous/security_precommit_test.go
cmd/datawatch/main.go
.datawatch/verify/security-scan-precommit-wire.sh"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

has internal/session/manager.go 'SetPreCommitScan'
grep -n 'commitOnCompletion' internal/session/manager.go | head -1 >/dev/null || bad "commitOnCompletion missing"
awk '/func .*commitOnCompletion/,/^}/' internal/session/manager.go | grep -q 'reCommitScan' || bad "commitOnCompletion does not call the pre-commit scan"
has cmd/datawatch/main.go 'SetPreCommitScan'
has internal/autonomous/security_precommit.go 'func RunPreCommitSecurityScan'
has internal/autonomous/models.go 'SecurityFindingCount'
has internal/autonomous/models.go 'security_finding_count'
go build ./internal/session/ ./internal/autonomous/ ./cmd/datawatch/ || bad "go build"
go test ./internal/session/ -count=1 -run 'PreCommit' || bad "session PreCommit tests"
go test ./internal/autonomous/ -count=1 -run 'PreCommit|SecurityFindings' || bad "autonomous PreCommit/SecurityFindings tests"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "security-scan-precommit-wire.sh: FAIL"; exit 1; }
echo "security-scan-precommit-wire.sh: PASS"
