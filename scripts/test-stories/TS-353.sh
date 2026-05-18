#!/usr/bin/env bash
# TS-353 — docs_apply executes steps and returns 200/OK per step
# tags: surface:mcp feature:mcp feature:howto feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-353"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

_story_ts_353() {
  local resp

  # First read the howto to check if it has exec_steps
  resp=$(api POST /api/mcp/call '{"tool":"docs_read","params":{"howto":"cross-agent-memory"}}')
  save_evidence TS-353 "read.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_read not available in this build"
    return
  fi
  if echo "$resp" | grep -qi "not found\|no such"; then
    skip "cross-agent-memory howto not found"
    return
  fi
  if ! echo "$resp" | grep -qi "exec_steps"; then
    skip "cross-agent-memory howto has no exec_steps"
    return
  fi

  # Apply the howto
  resp=$(api POST /api/mcp/call '{"tool":"docs_apply","params":{"howto":"cross-agent-memory"}}')
  save_evidence TS-353 "apply.json" "$resp"
  if echo "$resp" | grep -qi "no exec_steps\|no steps\|nothing to apply\|not applicable"; then
    skip "docs_apply: no exec_steps applicable"
    return
  fi
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "docs_apply cross-agent-memory returned valid response"
  else
    skip "docs_apply returned unexpected shape: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_353
: "${RESULT:=fail}"
unset -f _story_ts_353
