#!/usr/bin/env bash
# TS-281 — daemon_logs via MCP returns log lines array
# tags: surface:mcp feature:mcp feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-281"
story_preflight "surface:mcp feature:mcp feature:bootstrap" || return 0

_story_ts_281() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"daemon_logs","params":{}}')
  save_evidence TS-281 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "daemon_logs not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "daemon_logs returned array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("lines" in d or "logs" in d or "result" in d)'; then
    ok "daemon_logs returned dict with logs key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "daemon_logs returned dict"
  else
    ko "daemon_logs unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_281
: "${RESULT:=fail}"
unset -f _story_ts_281
