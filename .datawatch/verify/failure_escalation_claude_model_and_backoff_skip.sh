#!/usr/bin/env bash
# Verification: a model-less claude-code escalation step resolves to sonnet and a backed-off backend is skipped; tests exist and pass.
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
    internal/autonomous/*.go|internal/config/*.go|internal/server/*.go|internal/server/web/docs/*|internal/server/web/docs/*/*|docs/*|docs/*/*|scripts/check-*.sh) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

[ -f internal/autonomous/failure_escalation_claude_step_test.go ] || bad "internal/autonomous/failure_escalation_claude_step_test.go is missing"
go build ./internal/autonomous/ || bad "go build"
go test ./internal/autonomous/ -count=1 -run 'FailureEscalation' || bad "failure escalation tests"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "failure_escalation_claude_model_and_backoff_skip.sh: FAIL"; exit 1; }
echo "failure_escalation_claude_model_and_backoff_skip.sh: PASS"
