#!/usr/bin/env bash
# TS-786 — Federation auth-failure must be REAL, not a false positive.
# Pins the actual bug class found live 2026-10-08 (v8.73.2-v8.73.4): a
# federated peer entry with a missing/wrong token must produce a real,
# detectable failure on both (a) the data-fetch proxy path and (b) the
# dedicated "verify this will work" Test endpoint -- neither may report
# success just because the remote process happens to be reachable.
# Depends on TS-785 (reuses its already-running second real daemon).
# tags: surface:api feature:federation group:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-786"
story_preflight "surface:api feature:federation" || return 0

_story_ts_786() {
  if [[ -z "${FED_PEER_HTTP:-}" ]]; then
    skip "TS-785 did not leave a running federation peer (FED_PEER_HTTP unset) -- depends on TS-785"
    return
  fi

  # Local aggregate: ok()/ko()/skip() each set the shared RESULT var to
  # only their OWN outcome (lib.sh:185-189) -- a story with multiple
  # checks silently reports just its LAST call's result as RESULT,
  # masking any earlier ko() as an overall pass. Found live while writing
  # this very story (its first version did exactly that). Track failures
  # explicitly and set the real RESULT at the end instead of trusting
  # whichever ok()/ko() happened to run last.
  local any_fail=0

  local bad_name="e2e-fed-peer-badtoken"
  local reg
  reg=$(api POST /api/servers "{\"name\":\"$bad_name\",\"url\":\"http://127.0.0.1:$FED_PEER_HTTP\",\"token\":\"wrong-token-entirely\",\"enabled\":true,\"federated\":true,\"capabilities\":[\"full-control\"]}")
  save_evidence "TS-786" "0_register_bad_peer.json" "$reg"

  # (a) The data-fetch path -- this is the exact call the PWA's
  # _checkFederatedConnection now makes (v8.73.3). Must be 401, not 200.
  local code
  code=$(curl -sk --max-time 10 -o /dev/null -w '%{http_code}' "$TEST_BASE/api/proxy/$bad_name/api/sessions" \
    -H "Authorization: Bearer $TEST_TOKEN")
  if [[ "$code" == "401" ]]; then
    ok "GET /api/proxy/<bad-token-peer>/api/sessions correctly returns 401, not a false-positive 200"
  else
    ko "expected 401 for a bad-token peer's sessions fetch, got $code (false positive if 200)"
    any_fail=1
  fi

  # Sanity: the SAME remote via /api/health (deliberately unauthenticated)
  # still returns 200 even with the bad token -- confirms this is testing
  # the real auth distinction, not just "is the remote up."
  code=$(curl -sk --max-time 10 -o /dev/null -w '%{http_code}' "$TEST_BASE/api/proxy/$bad_name/api/health" \
    -H "Authorization: Bearer $TEST_TOKEN")
  if [[ "$code" == "200" ]]; then
    ok "control check: /api/health still 200 for the same bad-token peer (confirms health alone can't distinguish auth failure)"
  else
    ko "expected the health control-check to be 200 (it's deliberately public); got $code -- if this fails too, the test setup itself is wrong, not just auth"
    any_fail=1
  fi

  # (b) The dedicated "verify this will work" surface -- multiserver.Store.Test(),
  # fixed in v8.73.4 for exactly this false-positive. Must report an error.
  local test_resp ok_field
  test_resp=$(api POST "/api/servers/$bad_name/test" '{}')
  save_evidence "TS-786" "1_server_test_endpoint.json" "$test_resp"
  ok_field=$(echo "$test_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('ok'))" 2>/dev/null || echo "")
  if [[ "$ok_field" == "False" || "$ok_field" == "false" ]]; then
    ok "POST /api/servers/<name>/test correctly reports ok=false for a bad-token peer (v8.73.4 regression pin)"
  else
    ko "POST /api/servers/<name>/test reported ok=$ok_field for a bad-token peer -- false positive, the v8.73.4 fix regressed"
    any_fail=1
  fi

  if [[ "$any_fail" == "1" ]]; then
    RESULT=fail
  else
    RESULT=pass
  fi
}

RESULT=fail
_story_ts_786
: "${RESULT:=fail}"
unset -f _story_ts_786
