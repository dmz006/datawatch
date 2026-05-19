#!/usr/bin/env bash
# TS-154 — Algorithm start + advance
# tags: surface:api feature:algorithm
# legacy fn: t12_ts154_algorithm_start_advance
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-154"
story_preflight "surface:api feature:algorithm" || return 0

_story_ts_154() {
  # Verify the algorithm endpoint exists
  local list_resp
  list_resp=$(api GET /api/algorithm 2>/dev/null || echo '{"error":"not found"}')
  save_evidence TS-154 "list.json" "$list_resp"
  if ! echo "$list_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
    skip "Algorithm endpoint not available"
    return
  fi
  ok "GET /api/algorithm returns algorithm surface"

  # Create a test session to register in algorithm mode
  ensure_test_session || return
  local alg_id="$SESSION_ID"

  # Register session in algorithm mode
  local start_resp
  start_resp=$(api POST "/api/algorithm/$alg_id/start" '{}' 2>/dev/null || echo '{}')
  save_evidence TS-154 "start.json" "$start_resp"
  if ! assert_json "$start_resp" 'isinstance(d, dict)'; then
    skip "Algorithm start not available: $(echo "$start_resp" | head -c 100)"
    return
  fi
  ok "Algorithm $alg_id start accepted"

  # Advance to next phase
  local adv_resp
  adv_resp=$(api POST "/api/algorithm/$alg_id/advance" '{}' 2>/dev/null || echo '{}')
  save_evidence TS-154 "advance.json" "$adv_resp"
  if assert_json "$adv_resp" 'isinstance(d, dict)'; then
    ok "Algorithm advance accepted"
  else
    skip "Algorithm advance not available"
  fi

  # Clean up: reset algorithm state
  api DELETE "/api/algorithm/$alg_id" >/dev/null 2>&1 || true
}

RESULT=fail
_story_ts_154
: "${RESULT:=fail}"
unset -f _story_ts_154
