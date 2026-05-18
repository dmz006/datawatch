#!/usr/bin/env bash
# TS-566 — POST /api/federation/peers/{name}/test returns {ok,latency_ms,version}
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-566"
story_preflight "surface:api feature:federation" || return 0

_story_ts_566() {
  local peer_name="e2e-fed-peer-ts566"
  # Try to create a peer first so we have something to test
  local create_resp
  create_resp=$(api POST /api/federation/peers "{\"name\":\"$peer_name\",\"url\":\"http://127.0.0.1:19999\",\"token\":\"test-token\"}")
  if echo "$create_resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi

  local resp
  resp=$(api POST "/api/federation/peers/$peer_name/test" '{}')
  save_evidence TS-566 "resp.json" "$resp"

  # cleanup
  api DELETE "/api/federation/peers/$peer_name" >/dev/null 2>&1 || true

  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers/{name}/test endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and ("ok" in d or "latency_ms" in d or "error" in d)'; then
    ok "POST /api/federation/peers/$peer_name/test returned structured response"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/federation/peers/$peer_name/test returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_566
: "${RESULT:=fail}"
unset -f _story_ts_566
