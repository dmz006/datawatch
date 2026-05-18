#!/usr/bin/env bash
# TS-481 — DELETE /api/llms/{name} returns 409 when active bindings exist
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-481"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_481() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  # Check if there are active bindings first
  local in_use_resp
  in_use_resp=$(api GET "/api/llms/$llm_name/in_use")
  local has_bindings
  has_bindings=$(echo "$in_use_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);b=d.get("bindings",[]);print("yes" if isinstance(b,list) and len(b)>0 else "no")' 2>/dev/null || echo "no")
  if [[ "$has_bindings" != "yes" ]]; then
    skip "requires active session binding — no bindings on $llm_name"
    return
  fi
  local resp code
  resp=$(api_code DELETE "/api/llms/$llm_name")
  save_evidence TS-481 "delete.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "409" ]]; then
    ok "DELETE /api/llms/$llm_name returns 409 with active bindings"
  elif [[ "$code" == "404" ]]; then
    skip "DELETE /api/llms endpoint not available"
  else
    ko "expected 409, got $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_481
: "${RESULT:=fail}"
unset -f _story_ts_481
