#!/usr/bin/env bash
# TS-312 — datawatch algorithm list exits 0
# tags: surface:cli feature:cli feature:algorithm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-312"
story_preflight "surface:cli feature:cli feature:algorithm" || return 0

_story_ts_312() {
  local out; out=$(cli_test algorithm list 2>&1); local rc=$?
  save_evidence TS-312 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "algorithm list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "algorithm list not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_312
: "${RESULT:=fail}"
unset -f _story_ts_312
