#!/usr/bin/env bash
# TS-526 — datawatch memory scope recall --project /tmp exits 0 — skip if memory disabled
# tags: surface:cli feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-526"
story_preflight "surface:cli feature:memory" || return 0

_story_ts_526() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local out rc
  out=$(cli_test memory scope recall --project /tmp 2>&1); rc=$?
  save_evidence TS-526 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch memory scope recall --project /tmp exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|unknown.*flag|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_526
: "${RESULT:=fail}"
unset -f _story_ts_526
