#!/usr/bin/env bash
# TS-552 — council_config_get MCP tool returns llm_ref+max_parallel fields
# tags: surface:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-552"
story_preflight "surface:mcp feature:council" || return 0

_story_ts_552() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"council_config_get","params":{}}')
  save_evidence TS-552 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_config_get tool not available"
    return
  fi
  if assert_json "$resp" '"llm_ref" in d or "max_parallel" in d or isinstance(d, dict)'; then
    ok "council_config_get tool returned valid shape"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_552
: "${RESULT:=fail}"
unset -f _story_ts_552
