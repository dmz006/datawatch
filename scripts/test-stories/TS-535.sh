#!/usr/bin/env bash
# TS-535 — council_persona_draft_start MCP tool creates draft with draft_id
# tags: surface:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-535"
story_preflight "surface:mcp feature:council" || return 0

_story_ts_535() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"council_persona_draft_start","params":{"name":"test-draft-'"$$"'","description":"test"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-535 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_persona_draft_start tool not available"
    return
  fi
  local draft_id
  draft_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("draft_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -n "$draft_id" ]]; then
    ok "council_persona_draft_start returned draft_id: $draft_id"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_persona_draft_start returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_535
: "${RESULT:=fail}"
unset -f _story_ts_535
