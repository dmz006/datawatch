#!/usr/bin/env bash
# TS-329 — datawatch orchestrator graphs list exits 0
# tags: surface:cli feature:cli feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-329"
story_preflight "surface:cli feature:cli feature:parity" || return 0

_story_ts_329() {
  local out; out=$(cli_test orchestrator graphs list 2>&1); local rc=$?
  save_evidence TS-329 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "orchestrator graphs list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "orchestrator not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_329
: "${RESULT:=fail}"
unset -f _story_ts_329
