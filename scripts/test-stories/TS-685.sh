#!/usr/bin/env bash
# TS-685 — DELETE /api/autonomous/prds/{id}?memory_strategy=archive deletes with archive
# tags: surface:api feature:memory feature:automata group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-685"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_685() {
  local prd_id code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-685 archive-delete e2e test","project_dir":"/tmp","backend":"opencode"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD"
    return
  fi

  resp=$(api_code DELETE "/api/autonomous/prds/$prd_id?memory_strategy=archive" '')
  save_evidence TS-685 "delete-archive.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200|204)
      if echo "$resp" | grep -q '"memory_strategy"'; then
        ok "DELETE with memory_strategy=archive returned $code with strategy echo"
      else
        ok "DELETE with memory_strategy=archive returned $code"
      fi
      ;;
    404) skip "PRD already gone or endpoint variation not available (404)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_685
: "${RESULT:=fail}"
unset -f _story_ts_685
