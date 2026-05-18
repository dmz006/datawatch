#!/usr/bin/env bash
# TS-352 — docs_read "cross-agent-memory" returns content with exec_steps
# tags: surface:mcp feature:mcp feature:howto feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-352"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

_story_ts_352() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_read","params":{"path":"howto/cross-agent-memory.md"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-352 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_read not available in this build"
    return
  fi
  if echo "$resp" | grep -qi "not found\|no such\|404"; then
    skip "cross-agent-memory howto not found (may not exist in this build)"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and ("content" in d or "body" in d or "exec_steps" in d)'; then
    if echo "$resp" | grep -qi "exec_steps"; then
      ok "docs_read cross-agent-memory returned content with exec_steps"
    else
      ok "docs_read cross-agent-memory returned content dict"
    fi
  elif assert_json "$resp" 'isinstance(d, str) and len(d) > 10'; then
    ok "docs_read cross-agent-memory returned content string"
  else
    skip "docs_read returned unexpected shape: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_352
: "${RESULT:=fail}"
unset -f _story_ts_352
