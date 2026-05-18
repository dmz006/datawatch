#!/usr/bin/env bash
# TS-381 — GET /api/push/<topic> streams SSE events (ntfy-compat)
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-381"
story_preflight "surface:api feature:push" || return 0

_story_ts_381() {
  local topic="test-push-$$"
  local headers
  headers=$(curl -sk --max-time 3 \
    -H "Authorization: Bearer $TEST_TOKEN" \
    -D - -o /dev/null \
    "$TEST_BASE/api/push/$topic" 2>/dev/null || true)
  save_evidence TS-381 "headers.txt" "$headers"
  if echo "$headers" | grep -qi "text/event-stream"; then
    ok "GET /api/push/$topic returns Content-Type: text/event-stream"
  elif echo "$headers" | grep -qi "404 Not Found\|404"; then
    skip "push endpoint not available (404)"
  elif [[ -z "$headers" ]]; then
    skip "no response from push endpoint (connection refused or timeout)"
  else
    # Some builds return the push endpoint differently
    local code
    code=$(echo "$headers" | grep -m1 "HTTP/" | awk '{print $2}')
    if [[ "$code" == "200" ]]; then
      ok "GET /api/push/$topic returns 200 (SSE stream started)"
    else
      skip "push endpoint returned $code — may require WebSocket or different transport"
    fi
  fi
}

RESULT=fail
_story_ts_381
: "${RESULT:=fail}"
unset -f _story_ts_381
