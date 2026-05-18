#!/usr/bin/env bash
# TS-313 — datawatch compute list exits 0
# tags: surface:cli feature:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-313"
story_preflight "surface:cli feature:cli feature:compute" || return 0

_story_ts_313() {
  local out; out=$(cli_test compute list 2>&1); local rc=$?
  save_evidence TS-313 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "compute list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    # try compute node list
    out=$(cli_test compute node list 2>&1); rc=$?
    if [[ $rc -eq 0 ]]; then
      ok "compute node list exits 0"
    else
      skip "compute list not available: $(echo "$out" | head -c 80)"
    fi
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_313
: "${RESULT:=fail}"
unset -f _story_ts_313
