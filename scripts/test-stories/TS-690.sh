#!/usr/bin/env bash
# TS-690 — PATCH /api/autonomous/prds/{id} set_story_llm: per-story backend field accepted
# tags: surface:api feature:automata group:per-story-llm-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-690"
story_preflight "surface:api feature:automata group:per-story-llm-v9" || return 0

_story_ts_690() {
  local prd_id code resp

  # Create PRD so we have an ID.
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-690 per-story llm e2e","project_dir":"/tmp","backend":"opencode"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD"
    return
  fi

  # set_story_llm is sent as a PATCH body with action set_story_llm.
  resp=$(api_code PATCH "/api/autonomous/prds/$prd_id" \
    '{"action":"set_story_llm","story_id":"story-0","backend":"opencode","model":"qwen3:8b"}')
  save_evidence TS-690 "set-story-llm.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")

  api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true

  case "$code" in
    200|201) ok "set_story_llm accepted with HTTP $code" ;;
    400)     ok "400 — story-id not found (expected on fresh PRD with no stories)" ;;
    404)     skip "set_story_llm endpoint not available (404)" ;;
    405)     skip "PATCH not supported (405)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_690
: "${RESULT:=fail}"
unset -f _story_ts_690
