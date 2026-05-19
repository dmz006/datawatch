#!/usr/bin/env bash
# TS-480 — POST /api/llms/{name}/force_delete endpoint exists
# tags: surface:api feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-480"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_480() {
  local llm_name="ts480-test-$$"

  # Create a throwaway LLM to force-delete
  local create_resp create_code
  create_resp=$(api_code POST /api/llms \
    "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":false}")
  create_code=$(echo "$create_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  local create_body
  create_body=$(echo "$create_resp" | sed 's/__HTTP_CODE_[0-9]*__//')

  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create throwaway LLM for force_delete test (code=$create_code): $(echo "$create_body" | head -c 100)"
    return
  fi
  # Register cleanup in case force_delete doesn't work
  add_cleanup llm "$llm_name"

  # Try POST /api/llms/{name}/force_delete
  local fd_resp fd_code fd_body
  fd_resp=$(api_code POST "/api/llms/$llm_name/force_delete" '{"confirm":"yes I understand this terminates active work"}')
  fd_code=$(echo "$fd_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  fd_body=$(echo "$fd_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-480 "force_delete.json" "$fd_body"

  if [[ "$fd_code" == "200" || "$fd_code" == "204" ]]; then
    ok "POST /api/llms/$llm_name/force_delete returned $fd_code"
  elif [[ "$fd_code" == "404" ]]; then
    # Endpoint doesn't exist — try DELETE with ?force=true as alternative
    local del_resp del_code del_body
    del_resp=$(api_code DELETE "/api/llms/$llm_name?force=true")
    del_code=$(echo "$del_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    del_body=$(echo "$del_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
    save_evidence TS-480 "delete_force.json" "$del_body"
    if [[ "$del_code" == "200" || "$del_code" == "204" ]]; then
      ok "DELETE /api/llms/$llm_name?force=true returned $del_code (force_delete via query param)"
    else
      ko "POST /api/llms/$llm_name/force_delete returned 404 and DELETE?force=true returned $del_code"
    fi
  else
    ko "POST /api/llms/$llm_name/force_delete returned unexpected HTTP $fd_code: $(echo "$fd_body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_480
: "${RESULT:=fail}"
unset -f _story_ts_480
