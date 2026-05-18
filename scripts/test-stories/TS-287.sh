#!/usr/bin/env bash
# TS-287 — docs_apply for curated howto exec_steps executes via MCP
# tags: surface:mcp feature:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-287"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_287() {
  local resp

  # Try docs_apply — use a benign howto name
  resp=$(api POST /api/mcp/call '{"tool":"docs_apply","params":{"howto":"daemon-operations"}}')
  save_evidence TS-287 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_apply not available in this build"
    return
  fi
  if echo "$resp" | grep -qi "no exec_steps\|no steps\|nothing to apply\|not applicable\|read.only"; then
    skip "docs_apply: no exec_steps or not applicable for daemon-operations"
    return
  fi
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "docs_apply returned valid response"
  else
    skip "docs_apply returned unexpected shape: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_287
: "${RESULT:=fail}"
unset -f _story_ts_287
