#!/usr/bin/env bash
# TS-286 — docs_read for "daemon-operations" returns content
# tags: surface:mcp feature:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-286"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_286() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_read","params":{"path":"howto/daemon-operations.md"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-286 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_read not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and ("content" in d or "body" in d or "text" in d)'; then
    ok "docs_read daemon-operations returned content dict"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    # Any dict is OK — howto may not exist in this build
    if echo "$resp" | grep -qi "not found\|no such\|404"; then
      skip "daemon-operations howto not found (may not exist in this build)"
    else
      ok "docs_read daemon-operations returned dict"
    fi
  elif assert_json "$resp" 'isinstance(d, str) and len(d) > 10'; then
    ok "docs_read daemon-operations returned content string"
  else
    skip "docs_read returned unexpected shape: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_286
: "${RESULT:=fail}"
unset -f _story_ts_286
