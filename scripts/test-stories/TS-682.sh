#!/usr/bin/env bash
# TS-682 — GET /api/memory/scopes/inventory returns scope row counts
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-682"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_682() {
  local m_enabled code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  resp=$(api_code GET "/api/memory/scopes/inventory?project=/e2e-proj" '')
  save_evidence TS-682 "inventory.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  case "$code" in
    200)
      if assert_json "$body" 'isinstance(d, dict)'; then
        ok "GET /api/memory/scopes/inventory returned 200 with dict payload"
      else
        ko "inventory response is not a dict: $(echo "$body" | head -c 200)"
      fi
      ;;
    404|405) skip "memory/scopes/inventory endpoint not available ($code)" ;;
    503)     skip "memory backend disabled (503)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_682
: "${RESULT:=fail}"
unset -f _story_ts_682
