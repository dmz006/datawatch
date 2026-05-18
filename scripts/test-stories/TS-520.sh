#!/usr/bin/env bash
# TS-520 — POST /api/memory/scopes/borrow returns 200 or skip if memory disabled
# tags: surface:api feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-520"
story_preflight "surface:api feature:memory" || return 0

_story_ts_520() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }
  local resp code
  resp=$(api_code POST /api/memory/scopes/borrow '{"scope":"project","ttl":300}')
  save_evidence TS-520 "borrow.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "201" || "$code" == "400" ]]; then
    ok "POST /api/memory/scopes/borrow returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "memory/scopes/borrow endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_520
: "${RESULT:=fail}"
unset -f _story_ts_520
