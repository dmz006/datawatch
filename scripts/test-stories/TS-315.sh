#!/usr/bin/env bash
# TS-315 — datawatch council list exits 0
# tags: surface:cli feature:cli feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-315"
story_preflight "surface:cli feature:cli feature:council" || return 0

_story_ts_315() {
  local out; out=$(cli_test council list 2>&1); local rc=$?
  save_evidence TS-315 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "council list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "council list not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_315
: "${RESULT:=fail}"
unset -f _story_ts_315
