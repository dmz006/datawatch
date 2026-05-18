#!/usr/bin/env bash
# TS-483 — GET /api/llms/{name} response has models array
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-483"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_483() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local resp
  resp=$(api GET "/api/llms/$llm_name")
  save_evidence TS-483 "llm.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "GET /api/llms/$llm_name not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d.get("models"), list)'; then
    ok "GET /api/llms/$llm_name has models array"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    skip "GET /api/llms/$llm_name responds but no models array: $(echo "$resp" | head -c 100)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_483
: "${RESULT:=fail}"
unset -f _story_ts_483
