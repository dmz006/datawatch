#!/usr/bin/env bash
# TS-337 — datawatch cost summary exits 0
# tags: surface:cli feature:cli feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-337"
story_preflight "surface:cli feature:cli feature:config" || return 0

_story_ts_337() {
  local out; out=$(cli_test cost summary 2>&1); local rc=$?
  save_evidence TS-337 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "cost summary exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "cost summary not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_337
: "${RESULT:=fail}"
unset -f _story_ts_337
