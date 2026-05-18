#!/usr/bin/env bash
# TS-523 — GET /api/memory/scopes/recall — skip if memory disabled
# tags: surface:api feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-523"
story_preflight "surface:api feature:memory" || return 0

_story_ts_523() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local resp
  resp=$(api GET "/api/memory/scopes/recall?scope=project&query=test")
  save_evidence TS-523 "recall.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "memory/scopes/recall endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/memory/scopes/recall returned valid JSON"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_523
: "${RESULT:=fail}"
unset -f _story_ts_523
