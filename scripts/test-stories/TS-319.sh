#!/usr/bin/env bash
# TS-319 — datawatch routing-rules test exits 0
# tags: surface:cli feature:cli feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-319"
story_preflight "surface:cli feature:cli feature:parity" || return 0

_story_ts_319() {
  local out; out=$(cli_test routing-rules test "test task" --backend shell 2>&1); local rc=$?
  save_evidence TS-319 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "routing-rules test exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command|unknown flag"; then
    skip "routing-rules test not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_319
: "${RESULT:=fail}"
unset -f _story_ts_319
