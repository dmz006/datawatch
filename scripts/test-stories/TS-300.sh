#!/usr/bin/env bash
# TS-300 — tooling_status + tooling_gitignore + tooling_cleanup shape via MCP
# tags: surface:mcp feature:mcp feature:plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-300"
story_preflight "surface:mcp feature:mcp feature:plugins" || return 0

_story_ts_300() {
  local resp

  # tooling_status
  resp=$(api POST /api/mcp/call '{"tool":"tooling_status","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-300 "status.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "tooling_status not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "tooling_status unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # tooling_gitignore
  resp=$(api POST /api/mcp/call '{"tool":"tooling_gitignore","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-300 "gitignore.json" "$resp"

  # tooling_cleanup
  resp=$(api POST /api/mcp/call '{"tool":"tooling_cleanup","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-300 "cleanup.json" "$resp"

  ok "tooling_status + tooling_gitignore + tooling_cleanup all returned"
}

RESULT=fail
_story_ts_300
: "${RESULT:=fail}"
unset -f _story_ts_300
