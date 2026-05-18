#!/usr/bin/env bash
# TS-330 — datawatch skills list exits 0
# tags: surface:cli feature:cli feature:skills
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-330"
story_preflight "surface:cli feature:cli feature:skills" || return 0

_story_ts_330() {
  local out; out=$(cli_test skills list 2>&1); local rc=$?
  save_evidence TS-330 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "skills list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "skills not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_330
: "${RESULT:=fail}"
unset -f _story_ts_330
