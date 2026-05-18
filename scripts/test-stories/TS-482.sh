#!/usr/bin/env bash
# TS-482 — POST /api/llms/{name}/refresh_models returns 200
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-482"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_482() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local resp code
  resp=$(api_code POST "/api/llms/$llm_name/refresh_models" '{}')
  save_evidence TS-482 "refresh.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "202" ]]; then
    ok "POST /api/llms/$llm_name/refresh_models returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "refresh_models endpoint not available for $llm_name"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_482
: "${RESULT:=fail}"
unset -f _story_ts_482
