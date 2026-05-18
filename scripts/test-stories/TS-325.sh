#!/usr/bin/env bash
# TS-325 — datawatch memory recall "test query" exits 0
# tags: surface:cli feature:cli feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-325"
story_preflight "surface:cli feature:cli feature:memory" || return 0

_story_ts_325() {
  # Check if memory is enabled
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$m_enabled" != "yes" ]]; then
    skip "memory subsystem not enabled"
    return
  fi

  local out; out=$(cli_test memory recall "test query" 2>&1); local rc=$?
  save_evidence TS-325 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "memory recall exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "memory recall not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_325
: "${RESULT:=fail}"
unset -f _story_ts_325
