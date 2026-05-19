#!/usr/bin/env bash
# TS-481 — DELETE /api/llms/{name} returns 409 when active bindings exist
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-481"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_481() {
  # Create a test LLM with a unique name
  local llm_name="ts481-test-llm-$$"
  local create_code create_body
  create_body=$(api_code POST /api/llms "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":true}")
  create_code=$(echo "$create_body" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    skip "could not create test LLM (code=$create_code) — llm registry not available"
    return
  fi
  add_cleanup llm "$llm_name"

  # Start a shell session with that LLM to create an active binding
  local sess_resp sess_id
  sess_resp=$(api POST /api/sessions/start \
    "{\"task\":\"ts481-binding-test\",\"backend\":\"shell\",\"llm\":\"$llm_name\",\"project_dir\":\"/tmp\"}")
  sess_id=$(echo "$sess_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id","") or d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sess_id" ]]; then
    # Cleanup LLM and skip
    api DELETE "/api/llms/$llm_name" >/dev/null 2>&1
    skip "could not create session for LLM binding test"
    return
  fi
  add_cleanup sess "$sess_id"

  # Verify binding is active
  local in_use_resp has_bindings
  in_use_resp=$(api GET "/api/llms/$llm_name/in_use")
  has_bindings=$(echo "$in_use_resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
b=d.get("sessions",[])
print("yes" if isinstance(b,list) and len(b)>0 else "no")
' 2>/dev/null || echo "no")
  save_evidence TS-481 "in_use.json" "$in_use_resp"

  if [[ "$has_bindings" != "yes" ]]; then
    skip "session did not create a binding on $llm_name — binding may not be enforced for shell sessions"
    return
  fi

  # Try to delete the LLM with active binding — expect 409
  local del_resp del_code del_body
  del_resp=$(api_code DELETE "/api/llms/$llm_name")
  del_code=$(echo "$del_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  del_body=$(echo "$del_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-481 "delete.json" "$del_body"

  # Kill session so cleanup can proceed
  api POST /api/sessions/kill "{\"id\":\"$sess_id\"}" >/dev/null 2>&1

  if [[ "$del_code" == "409" ]]; then
    ok "DELETE /api/llms/$llm_name returns 409 when session binding active"
  elif [[ "$del_code" == "200" ]]; then
    ko "DELETE returned 200 — API should reject deletion with active binding (409)"
  elif [[ "$del_code" == "404" ]]; then
    skip "DELETE /api/llms endpoint not available (404)"
  else
    ko "unexpected HTTP $del_code: $del_body"
  fi
}

RESULT=fail
_story_ts_481
: "${RESULT:=fail}"
unset -f _story_ts_481
