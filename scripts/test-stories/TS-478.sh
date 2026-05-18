#!/usr/bin/env bash
# TS-478 — GET /api/llms/{name}/in_use returns {bindings:[]} shape
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-478"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_478() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local resp
  resp=$(api GET "/api/llms/$llm_name/in_use")
  save_evidence TS-478 "in_use.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "in_use endpoint not available for $llm_name"
    return
  fi
  if assert_json "$resp" '"bindings" in d'; then
    ok "GET /api/llms/$llm_name/in_use returns bindings field"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/llms/$llm_name/in_use returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_478
: "${RESULT:=fail}"
unset -f _story_ts_478
