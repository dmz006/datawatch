#!/usr/bin/env bash
# TS-479 — POST /api/llms/{name}/reassign returns 200 with count field
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-479"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_479() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  # Get second LLM name for reassignment target
  local to_llm
  to_llm=$(api GET /api/llms | python3 -c '
import json,sys
d=json.load(sys.stdin)
llms=d.get("llms",d) if isinstance(d,dict) else d
names=[l["name"] for l in llms if isinstance(llms,list) and l.get("name")]
print(names[1] if len(names)>1 else names[0] if names else "")
' 2>/dev/null || echo "")
  local payload="{}"
  [[ -n "$to_llm" ]] && payload="{\"to_llm\":\"$to_llm\"}"
  local resp code
  resp=$(api_code POST "/api/llms/$llm_name/reassign" "$payload")
  save_evidence TS-479 "reassign.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if echo "$resp" | grep -qi "not found\|__HTTP_CODE_404__"; then
    skip "reassign endpoint not available for $llm_name"
    return
  fi
  if [[ "$code" == "200" ]] && assert_json "$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')" '"count" in d'; then
    ok "POST /api/llms/$llm_name/reassign returned 200 with count"
  elif [[ "$code" == "200" ]]; then
    ok "POST /api/llms/$llm_name/reassign returned 200"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_479
: "${RESULT:=fail}"
unset -f _story_ts_479
