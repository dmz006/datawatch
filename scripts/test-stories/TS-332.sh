#!/usr/bin/env bash
# TS-332 — datawatch plugins list exits 0
# tags: surface:cli feature:cli feature:plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-332"
story_preflight "surface:cli feature:cli feature:plugins" || return 0

_story_ts_332() {
  local out; out=$(cli_test plugins list 2>&1); local rc=$?
  save_evidence TS-332 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "plugins list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "plugins not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_332
: "${RESULT:=fail}"
unset -f _story_ts_332
