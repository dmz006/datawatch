#!/usr/bin/env bash
# TS-664 — POST /api/vision/describe unit tests: 6 tests pass
# tags: surface:unit feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-664"
story_preflight "surface:unit feature:vision group:vision parallel:ok" || return 0

_story_ts_664() {
  local out rc
  out=$(cd "$REPO_ROOT" && go test ./internal/server/... -run Vision -v -count=1 2>&1); rc=$?
  save_evidence TS-664 "test_output.txt" "$out"

  if [[ $rc -ne 0 ]]; then
    ko "server vision handler tests failed: $(echo "$out" | grep -E "^---FAIL|FAIL" | head -5)"
    return
  fi
  local count
  count=$(echo "$out" | grep -c "^--- PASS" || true)
  if [[ $count -lt 6 ]]; then
    ko "expected ≥6 passing server vision tests, got $count"
    return
  fi
  ok "POST /api/vision/describe unit tests: $count passed"
}

RESULT=fail
_story_ts_664
: "${RESULT:=fail}"
unset -f _story_ts_664
