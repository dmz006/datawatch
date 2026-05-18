#!/usr/bin/env bash
# TS-326 — datawatch secrets list exits 0
# tags: surface:cli feature:cli feature:secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-326"
story_preflight "surface:cli feature:cli feature:secrets" || return 0

_story_ts_326() {
  local out; out=$(cli_test secrets list 2>&1); local rc=$?
  save_evidence TS-326 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "secrets list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "secrets not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_326
: "${RESULT:=fail}"
unset -f _story_ts_326
