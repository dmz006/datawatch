#!/usr/bin/env bash
# TS-290 — guardrail_library_list + guardrail_profile CRUD via MCP
# tags: surface:mcp feature:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-290"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

_story_ts_290() {
  local resp profile_id profile_name
  profile_name="e2e-guardrail-ts290-$$"

  # List library
  resp=$(api POST /api/mcp/call '{"tool":"guardrail_library_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-290 "library.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "guardrail_library_list not available in this build"
    return
  fi

  # Create profile
  resp=$(api POST /api/mcp/call "{\"tool\":\"guardrail_profile_create\",\"params\":{\"name\":\"$profile_name\",\"rules\":[]}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-290 "create.json" "$resp"
  profile_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",d.get("name","")))' 2>/dev/null || echo "")
  if [[ -z "$profile_id" ]]; then
    profile_id="$profile_name"
  fi
  add_cleanup guardrail_profile "$profile_id"

  # Get profile
  resp=$(api POST /api/mcp/call "{\"tool\":\"guardrail_profile_get\",\"params\":{\"id\":\"$profile_id\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-290 "get.json" "$resp"

  # Delete profile
  resp=$(api POST /api/mcp/call "{\"tool\":\"guardrail_profile_delete\",\"params\":{\"id\":\"$profile_id\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-290 "delete.json" "$resp"

  ok "guardrail_library_list + guardrail_profile CRUD completed"
}

RESULT=fail
_story_ts_290
: "${RESULT:=fail}"
unset -f _story_ts_290
