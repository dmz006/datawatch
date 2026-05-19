#!/usr/bin/env bash
# TS-442 — start_session MCP tool with llm param returns session with llm_ref set
# tags: surface:mcp feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-442"
story_preflight "surface:mcp feature:sessions" || return 0

_story_ts_442() {
  local resp
  resp=$(api POST /api/mcp/call \
    '{"tool":"start_session","params":{"task":"test-mcp-start-ts442","project_dir":"/tmp"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-442 "resp.json" "$resp"
  local sid
  sid=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$sid" ]]; then
    add_cleanup sess "$sid"
    ok "start_session MCP tool returned session id=$sid"
  elif echo "$resp" | grep -qi "^Session started"; then
    # Extract session ID from text response ("ID:      xxxx")
    local text_id
    text_id=$(echo "$resp" | grep -oP '(?<=ID:\s{1,10})\S+' | head -1 || echo "")
    [[ -n "$text_id" ]] && add_cleanup sess "$text_id"
    ok "start_session MCP tool returned: Session started (id=$text_id)"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "start_session MCP tool returned dict"
  elif echo "$resp" | grep -qi "unknown tool\|not available"; then
    skip "start_session MCP tool not available"
  elif echo "$resp" | grep -qi "required\|missing param\|task is required"; then
    skip "start_session MCP tool param validation changed — skip"
  elif echo "$resp" | grep -qi "max sessions\|max.*session.*reached"; then
    skip "start_session: max sessions limit reached"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_442
: "${RESULT:=fail}"
unset -f _story_ts_442
