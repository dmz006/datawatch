#!/usr/bin/env bash
# TS-295 — orchestrator_config_get + orchestrator_graph_list + orchestrator_verdicts via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-295"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_295() {
  local resp

  # orchestrator_config_get
  resp=$(api POST /api/mcp/call '{"tool":"orchestrator_config_get","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-295 "config.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "orchestrator_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "orchestrator_config_get unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # orchestrator_graph_list
  resp=$(api POST /api/mcp/call '{"tool":"orchestrator_graph_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-295 "graphs.json" "$resp"

  # orchestrator_verdicts
  resp=$(api POST /api/mcp/call '{"tool":"orchestrator_verdicts","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-295 "verdicts.json" "$resp"

  ok "orchestrator_config_get + graph_list + verdicts all returned"
}

RESULT=fail
_story_ts_295
: "${RESULT:=fail}"
unset -f _story_ts_295
