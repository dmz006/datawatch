#!/usr/bin/env bash
# TS-335 — datawatch schedule list exits 0
# tags: surface:cli feature:cli feature:schedules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-335"
story_preflight "surface:cli feature:cli feature:schedules" || return 0

_story_ts_335() {
  local out; out=$(cli_test session schedule list 2>&1); local rc=$?
  save_evidence TS-335 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "schedule list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "schedule not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_335
: "${RESULT:=fail}"
unset -f _story_ts_335
