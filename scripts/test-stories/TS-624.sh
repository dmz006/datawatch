#!/usr/bin/env bash
# TS-624 — v8.0 smoke — unknown token on MCP SSE returns 401
# tags: surface:mcp feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-624"
story_preflight "surface:mcp feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_624() {
  local code
  local _bad_tok="completely-wrong-token-xyz" # gitleaks:allow — intentionally wrong, tests 401 rejection
  code=$(curl -sk --max-time 3 \
    "http://127.0.0.1:$TEST_MCP_PORT/sse" \
    -H "Authorization: Bearer ${_bad_tok}" \
    -w "%{http_code}" -o /dev/null 2>/dev/null || echo "000")
  save_evidence TS-624 "mcp_bad_token_code.txt" "$code"

  if [[ "$code" == "401" ]]; then
    ok "MCP SSE returns 401 for unknown token"
  else
    ko "expected 401 for unknown token on MCP SSE, got $code"
  fi
}

RESULT=fail
_story_ts_624
: "${RESULT:=fail}"
unset -f _story_ts_624
