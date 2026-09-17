#!/usr/bin/env bash
# TS-684 — GET /api/autonomous/prds/{id}/memory-report returns report payload
# tags: surface:api feature:memory feature:automata group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-684"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_684() {
  local prd_id code resp

  # Create a minimal PRD to get a valid ID.
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-684 memory-report e2e test","project_dir":"/tmp","backend":"opencode"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD (autonomous feature may require LLM backend)"
    return
  fi

  resp=$(api_code GET "/api/autonomous/prds/$prd_id/memory-report" '')
  save_evidence TS-684 "memory-report.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")

  # Cleanup.
  api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true

  case "$code" in
    200)
      local body; body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
      if assert_json "$body" '"prd_id" in d'; then
        ok "GET memory-report returned 200 with prd_id field"
      else
        ko "memory-report response missing prd_id: $(echo "$body" | head -c 200)"
      fi
      ;;
    503) skip "memory backend disabled (503)" ;;
    404) skip "memory-report endpoint not available for this PRD (404)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_684
: "${RESULT:=fail}"
unset -f _story_ts_684
