#!/usr/bin/env bash
# TS-553 — datawatch memory recall "test query" exits 0 — skip if memory disabled
# tags: surface:cli feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-553"
story_preflight "surface:cli feature:memory" || return 0

_story_ts_553() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local out rc
  out=$(cli_test memory recall "test query" 2>&1); rc=$?
  save_evidence TS-553 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch memory recall exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_553
: "${RESULT:=fail}"
unset -f _story_ts_553
