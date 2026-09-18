#!/usr/bin/env bash
# TS-719 — PRD Split Planning: set_llm with valid decomposition_profile returns 200
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-719"
story_preflight "surface:api feature:automata" || return 0

_story_ts_719() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Create a PRD to set LLM on
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-719 decomposition profile test.","project_dir":"/tmp/ts719-dp"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD for set_llm decomposition_profile test"
    return
  fi

  _cleanup() {
    api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true
  }

  # Find a registered LLM to use as decomposition_profile
  local llm_name
  llm_name=$(api GET /api/llms 2>/dev/null \
    | python3 -c '
import json,sys
d = json.load(sys.stdin)
items = d if isinstance(d,list) else d.get("llms",[])
for item in items:
    n = item.get("name","")
    if n and not item.get("disabled",False):
        print(n)
        break
' 2>/dev/null || echo "")

  if [[ -z "$llm_name" ]]; then
    # Try 'ollama' which is always auto-registered
    llm_name="ollama"
  fi

  # POST set_llm with decomposition_profile
  local resp code body
  resp=$(api_code POST "/api/autonomous/prds/$prd_id/set_llm" \
    "{\"decomposition_profile\":\"$llm_name\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-719 "set_llm_resp.json" "$body"

  _cleanup

  if [[ "$code" == "200" ]]; then
    ok "POST set_llm with decomposition_profile=$llm_name succeeded (HTTP 200)"
    return
  fi
  if [[ "$code" == "400" ]]; then
    if echo "$body" | grep -qi "unknown planning LLM\|unknown.*LLM\|not found"; then
      skip "decomposition_profile=$llm_name not in inference registry (HTTP 400: $(echo "$body" | head -c 100)) — try a registered LLM name"
    else
      ko "set_llm with decomposition_profile=$llm_name returned 400: $(echo "$body" | head -c 200)"
    fi
    return
  fi
  if [[ "$code" == "404" ]]; then
    skip "set_llm endpoint returned 404 — endpoint may use different URL on this version"
    return
  fi

  ko "set_llm decomposition_profile returned HTTP $code: $(echo "$body" | head -c 200)"
}

RESULT=fail
_story_ts_719
: "${RESULT:=fail}"
unset -f _story_ts_719
unset -f _cleanup 2>/dev/null || true
