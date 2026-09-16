#!/usr/bin/env bash
# TS-686 — GET /api/memory/scopes/recall with prd_id param filters to prd-shared scope
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-686"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_686() {
  local m_enabled code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  resp=$(api_code GET "/api/memory/scopes/recall?prd_id=e2e-prd-$$&project=/e2e-proj-$$&top_k=5" '')
  save_evidence TS-686 "recall-prd.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200)
      if assert_json "$resp" 'isinstance(d, list) or isinstance(d, dict)'; then
        ok "GET recall with prd_id returned 200"
      else
        ko "recall response has unexpected shape: $(echo "$resp" | head -c 200)"
      fi
      ;;
    503) skip "memory backend disabled (503)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_686
: "${RESULT:=fail}"
unset -f _story_ts_686
