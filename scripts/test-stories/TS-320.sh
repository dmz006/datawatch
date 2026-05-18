#!/usr/bin/env bash
# TS-320 — datawatch rtk check exits 0
# tags: surface:cli feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-320"
story_preflight "surface:cli feature:cli" || return 0

_story_ts_320() {
  local out; out=$(cli_test rtk check 2>&1); local rc=$?
  save_evidence TS-320 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "rtk check exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "rtk check not available in CLI: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_320
: "${RESULT:=fail}"
unset -f _story_ts_320
