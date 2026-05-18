#!/usr/bin/env bash
# TS-622 — v8.0 smoke — MCP SSE connects
# tags: surface:mcp feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-622"
story_preflight "surface:mcp feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_622() {
  local code
  code=$(curl "${curl_args[@]}" --max-time 3 \
    "http://127.0.0.1:$TEST_MCP_PORT/sse" \
    -w "%{http_code}" -o /dev/null 2>/dev/null || echo "000")
  save_evidence TS-622 "mcp_sse_code.txt" "$code"

  if [[ "$code" == "000" ]]; then
    ko "MCP SSE endpoint unreachable (connection refused or timeout)"
  elif [[ "$code" == "401" ]]; then
    ko "MCP SSE returned 401 (auth rejected with valid token)"
  else
    ok "MCP SSE endpoint reachable (HTTP $code)"
  fi
}

RESULT=fail
_story_ts_622
: "${RESULT:=fail}"
unset -f _story_ts_622
