#!/usr/bin/env bash
# TS-663 — vision service unit tests: 10 tests pass
# tags: surface:unit feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-663"
story_preflight "surface:unit feature:vision group:vision parallel:ok" || return 0

_story_ts_663() {
  local out rc
  out=$(cd "$REPO_ROOT" && go test ./internal/vision/... -v -count=1 2>&1); rc=$?
  save_evidence TS-663 "test_output.txt" "$out"

  if [[ $rc -ne 0 ]]; then
    ko "internal/vision unit tests failed: $(echo "$out" | grep -E "^---FAIL|FAIL" | head -5)"
    return
  fi
  local count
  count=$(echo "$out" | grep -c "^--- PASS" || true)
  if [[ $count -lt 10 ]]; then
    ko "expected ≥10 passing vision tests, got $count"
    return
  fi
  ok "vision service unit tests: $count passed"
}

RESULT=fail
_story_ts_663
: "${RESULT:=fail}"
unset -f _story_ts_663
