#!/usr/bin/env bash
# TS-720 — PRD Split Planning: set_llm with invalid decomposition_profile returns 400
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-720"
story_preflight "surface:api feature:automata" || return 0

_story_ts_720() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Create a PRD
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-720 invalid decomposition_profile test.","project_dir":"/tmp/ts720-dp"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD for invalid decomposition_profile test"
    return
  fi

  _cleanup() {
    api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true
  }

  # POST set_llm with a nonsensical decomposition_profile
  local resp code body
  resp=$(api_code POST "/api/autonomous/prds/$prd_id/set_llm" \
    '{"decomposition_profile":"this-llm-does-not-exist-at-all-xyz123"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-720 "set_llm_resp.json" "$body"

  _cleanup

  if [[ "$code" == "400" ]]; then
    ok "set_llm with invalid decomposition_profile returned HTTP 400 — unknown LLM correctly rejected"
    return
  fi
  if [[ "$code" == "200" ]]; then
    ko "set_llm with nonexistent decomposition_profile was ACCEPTED (HTTP 200) — should be 400: $(echo "$body" | head -c 200)"
    return
  fi
  if [[ "$code" == "404" ]]; then
    skip "set_llm endpoint returned 404 — endpoint may use different URL on this version"
    return
  fi

  ko "set_llm invalid decomposition_profile returned HTTP $code: $(echo "$body" | head -c 200)"
}

RESULT=fail
_story_ts_720
: "${RESULT:=fail}"
unset -f _story_ts_720
unset -f _cleanup 2>/dev/null || true
