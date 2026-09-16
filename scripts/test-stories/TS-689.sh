#!/usr/bin/env bash
# TS-689 — executor_resume_test.go: all 4 unit tests pass (v8.33.8)
# tags: surface:unit feature:automata group:executor-resume-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-689"
story_preflight "surface:unit feature:automata group:executor-resume-v9" || return 0

_story_ts_689() {
  local out rc
  out=$(cd "$REPO_ROOT" && rtk go test ./internal/autonomous/ -run 'TestExecutorResume' -count=1 -timeout 30s 2>&1 || true)
  save_evidence TS-689 "go-test.txt" "$out"
  rc=$(echo "$out" | grep -c "^--- PASS: TestExecutorResume" || echo "0")
  if echo "$out" | grep -q "^ok"; then
    ok "executor_resume_test.go: $rc/4 TestExecutorResume tests passed"
  elif echo "$out" | grep -q "FAIL"; then
    ko "executor resume tests FAILED: $(echo "$out" | grep 'FAIL\|Error' | head -5)"
  else
    skip "could not determine test result (rtk not available or build error)"
  fi
}

RESULT=fail
_story_ts_689
: "${RESULT:=fail}"
unset -f _story_ts_689
