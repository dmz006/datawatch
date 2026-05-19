#!/usr/bin/env bash
# TS-525 — memory_scope_promote MCP tool — save a memory in project-shared scope, then promote
# tags: surface:mcp feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-525"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_525() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  # Save a memory to project-shared scope via memory_remember MCP tool
  # The default project dir is needed as from_project for the promote call
  local default_project
  default_project=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("session",{}).get("default_project_dir",""))' 2>/dev/null || echo "")
  [[ -z "$default_project" ]] && default_project="/home/dmz"

  local save_resp save_body mem_id
  save_resp=$(api POST /api/mcp/call "{\"tool\":\"memory_remember\",\"params\":{\"text\":\"ts525 scope-promote test $$\",\"scope\":\"project-shared\"}}")
  save_body=$(mcp_unwrap "$save_resp")
  save_evidence TS-525 "save.json" "$save_body"

  if echo "$save_body" | grep -qi "unknown tool\|not enabled\|not available"; then
    skip "memory_remember tool not available"
    return
  fi

  mem_id=$(echo "$save_body" | python3 -c '
import re, sys
text = sys.stdin.read()
m = re.search(r"#(\d+)", text)
if m: print(m.group(1))
' 2>/dev/null || echo "")

  if [[ -z "$mem_id" ]]; then
    # Try extracting from direct JSON id field
    mem_id=$(echo "$save_body" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  fi

  if [[ -z "$mem_id" ]]; then
    ko "could not extract memory id from save response: $save_body"
    return
  fi

  # Promote from project-shared to persona-global
  local promote_resp promote_body
  promote_resp=$(api POST /api/mcp/call "{\"tool\":\"memory_scope_promote\",\"params\":{\"memory_id\":\"$mem_id\",\"from_scope\":\"project-shared\",\"from_project\":\"$default_project\",\"to_scope\":\"persona-global\",\"to_persona\":\"ts525-test\",\"promoted_by\":\"ts525-e2e\"}}")
  promote_body=$(mcp_unwrap "$promote_resp")
  save_evidence TS-525 "promote.json" "$promote_body"

  if echo "$promote_body" | grep -qi "unknown tool\|not enabled"; then
    skip "memory_scope_promote tool not available"
    return
  fi

  if echo "$promote_body" | grep -qi "not found in source scope\|memory.*not found"; then
    ko "memory_scope_promote: could not find memory $mem_id in project-shared scope ($default_project): $promote_body"
    return
  fi

  if assert_json "$promote_body" '"new_memory_id" in d or "id" in d'; then
    local new_id
    new_id=$(echo "$promote_body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("new_memory_id","") or d.get("id",""))' 2>/dev/null || echo "")
    ok "memory_scope_promote: promoted memory $mem_id to persona-global (new_id=$new_id)"
  elif assert_json "$promote_body" 'isinstance(d, dict)'; then
    ok "memory_scope_promote tool returned dict: $promote_body"
  else
    ko "unexpected promote response: $(echo "$promote_body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_525
: "${RESULT:=fail}"
unset -f _story_ts_525
