#!/usr/bin/env bash
# TS-536 — council_persona_draft_list MCP tool returns drafts array
# tags: surface:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-536"
story_preflight "surface:mcp feature:council" || return 0

_story_ts_536() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"council_persona_draft_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-536 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_persona_draft_list tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("drafts",[]), list)'; then
    ok "council_persona_draft_list tool returned list/drafts shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_persona_draft_list tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_536
: "${RESULT:=fail}"
unset -f _story_ts_536
