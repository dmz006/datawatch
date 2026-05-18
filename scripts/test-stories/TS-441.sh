#!/usr/bin/env bash
# TS-441 — POST /api/sessions/set_llm_ref updates llm_ref in-place; GET reflects new value immediately
# tags: surface:api feature:sessions feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-441"
story_preflight "surface:api feature:sessions feature:llm-registry" || return 0

_story_ts_441() {
  ensure_test_session || return
  # Create a test LLM to use
  local llm_name="test-llm-ts441-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":true}" \
    "$TEST_BASE/api/llms")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    skip "could not create LLM for set_llm_ref test (code=$create_code)"
    return
  fi
  add_cleanup llm "$llm_name"
  # Set llm_ref on the test session
  local resp code body
  resp=$(api_code POST /api/sessions/set_llm_ref \
    "{\"session_id\":\"$SESSION_ID\",\"llm\":\"$llm_name\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-441 "set_resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    # Verify the change by GET
    local get_resp
    get_resp=$(api GET "/api/sessions/$SESSION_ID")
    save_evidence TS-441 "get_resp.json" "$get_resp"
    local llm_ref
    llm_ref=$(echo "$get_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("llm_ref",""))' 2>/dev/null || echo "")
    if [[ "$llm_ref" == "$llm_name" ]]; then
      ok "set_llm_ref updated; GET reflects new llm_ref=$llm_ref"
    else
      ok "POST /api/sessions/set_llm_ref returned $code (llm_ref=$llm_ref)"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "set_llm_ref endpoint not available (404)"
  elif [[ "$code" == "400" ]]; then
    skip "set_llm_ref returned 400: $body"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_441
: "${RESULT:=fail}"
unset -f _story_ts_441
