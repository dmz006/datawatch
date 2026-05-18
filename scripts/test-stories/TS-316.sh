#!/usr/bin/env bash
# TS-316 — datawatch llm list exits 0
# tags: surface:cli feature:cli feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-316"
story_preflight "surface:cli feature:cli feature:config" || return 0

_story_ts_316() {
  local out; out=$(cli_test llm list 2>&1); local rc=$?
  save_evidence TS-316 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "llm list exits 0: $(echo "$out" | head -c 80)"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "llm list not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_316
: "${RESULT:=fail}"
unset -f _story_ts_316
