#!/usr/bin/env bash
# TS-666 — router vision injection unit tests: 5 tests pass
# tags: surface:unit feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-666"
story_preflight "surface:unit feature:vision group:vision parallel:ok" || return 0

_story_ts_666() {
  local out rc
  out=$(cd "$REPO_ROOT" && go test ./internal/router/... -run Vision -v -count=1 2>&1); rc=$?
  save_evidence TS-666 "test_output.txt" "$out"

  if [[ $rc -ne 0 ]]; then
    ko "router vision injection tests failed: $(echo "$out" | grep -E "^---FAIL|FAIL" | head -5)"
    return
  fi
  local count
  count=$(echo "$out" | grep -c "^--- PASS" || true)
  if [[ $count -lt 5 ]]; then
    ko "expected ≥5 passing router vision tests, got $count"
    return
  fi
  ok "router vision injection unit tests: $count passed"
}

RESULT=fail
_story_ts_666
: "${RESULT:=fail}"
unset -f _story_ts_666
