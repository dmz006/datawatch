#!/usr/bin/env bash
# TS-665 — skills manifest unit tests: 4 tests pass
# tags: surface:unit feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-665"
story_preflight "surface:unit feature:vision group:vision parallel:ok" || return 0

_story_ts_665() {
  local out rc
  out=$(cd "$REPO_ROOT" && go test ./internal/skills/... -run AcceptsImages -v -count=1 2>&1); rc=$?
  save_evidence TS-665 "test_output.txt" "$out"

  if [[ $rc -ne 0 ]]; then
    ko "skills manifest AcceptsImages tests failed: $(echo "$out" | grep -E "^---FAIL|FAIL" | head -5)"
    return
  fi
  local count
  count=$(echo "$out" | grep -c "^--- PASS" || true)
  if [[ $count -lt 4 ]]; then
    ko "expected ≥4 passing AcceptsImages tests, got $count"
    return
  fi
  ok "skills manifest unit tests: $count passed"
}

RESULT=fail
_story_ts_665
: "${RESULT:=fail}"
unset -f _story_ts_665
