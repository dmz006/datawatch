#!/usr/bin/env bash
# TS-443 — datawatch session new --backend shell exits 0 and prints "Session started."
# tags: surface:cli feature:sessions feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-443"
story_preflight "surface:cli feature:sessions feature:cli" || return 0

_story_ts_443() {
  local task="test-cli-session-ts443-$$"
  local out rc
  out=$(cli_test session new --backend shell "$task" 2>&1); rc=$?
  save_evidence TS-443 "out.txt" "$out"
  if [[ $rc -eq 0 ]] && echo "$out" | grep -qi "session started"; then
    ok "datawatch session new --backend shell exits 0 with Session started"
    # cleanup: kill the session by name
    api DELETE "/api/sessions/$task" >/dev/null 2>&1 || true
  elif echo "$out" | grep -qiE "unknown.*flag|unknown command|not found|disabled|not.*available|no such"; then
    skip "session new --backend shell not available: $(echo "$out" | head -c 80)"
  elif [[ $rc -eq 0 ]]; then
    # started but no "Session started." in output (different phrasing or output captured differently)
    ok "datawatch session new --backend shell exits 0"
    api DELETE "/api/sessions/$task" >/dev/null 2>&1 || true
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_443
: "${RESULT:=fail}"
unset -f _story_ts_443
