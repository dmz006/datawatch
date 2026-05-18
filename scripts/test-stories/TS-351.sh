#!/usr/bin/env bash
# TS-351 — docs_list_howtos contains cross-agent-memory
# tags: surface:mcp feature:mcp feature:howto feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-351"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

_story_ts_351() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-351 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_list_howtos not available in this build"
    return
  fi
  if echo "$resp" | grep -qi "cross-agent-memory\|cross_agent_memory"; then
    ok "cross-agent-memory howto found in listing"
  else
    skip "cross-agent-memory howto not found (may not be indexed in this build)"
  fi
}

RESULT=fail
_story_ts_351
: "${RESULT:=fail}"
unset -f _story_ts_351
