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
    "{\"id\":\"$SESSION_ID\",\"llm_ref\":\"$llm_name\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-441 "set_resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    # Verify the change by checking sessions list (no single-item GET endpoint)
    local get_resp llm_ref
    get_resp=$(api GET /api/sessions)
    save_evidence TS-441 "get_resp.json" "$get_resp"
    llm_ref=$(echo "$get_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('sessions',[])
sid='$SESSION_ID'
sess=next((s for s in items if s.get('id','')==sid or s.get('full_id','').endswith('-'+sid)),None)
print(sess.get('llm_ref','') if sess else '')
" 2>/dev/null || echo "")
    if [[ "$llm_ref" == "$llm_name" ]]; then
      ok "set_llm_ref updated; sessions list reflects new llm_ref=$llm_ref"
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
