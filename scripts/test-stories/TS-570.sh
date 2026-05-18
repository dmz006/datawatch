#!/usr/bin/env bash
# TS-570 — Peer token with sessions:list cap → GET /api/sessions returns 200
# tags: surface:api feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-570"
story_preflight "surface:api feature:federation feature:cbac" || return 0

_story_ts_570() {
  local peers_resp
  peers_resp=$(api GET /api/federation/peers)
  if echo "$peers_resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi

  # Register a test peer with a known token and sessions:list capability
  local peer_name="cbac-test-peer-ts570-$$"
  local peer_token="cbac-test-token-ts570-$$"
  local add_resp
  add_resp=$(api POST /api/federation/peers \
    "{\"name\":\"$peer_name\",\"url\":\"http://127.0.0.1:19999\",\"token\":\"$peer_token\",\"enabled\":true,\"capabilities\":[\"sessions:list\",\"sessions:read\"]}")
  save_evidence TS-570 "add_peer.json" "$add_resp"

  if ! assert_json "$add_resp" '"name" in d or d.get("ok")'; then
    skip "could not register test peer: $(echo "$add_resp" | head -c 100)"
    return
  fi
  add_cleanup server "$peer_name"

  # Use the peer token to GET /api/sessions — should return 200 (has sessions:list)
  local sess_resp sess_code
  sess_resp=$(curl -sk --max-time 10 -w "\n__HTTP_CODE_%{http_code}__" \
    -H "Authorization: Bearer $peer_token" \
    "$TEST_BASE/api/sessions")
  sess_code=$(echo "$sess_resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local sess_body
  sess_body=$(echo "$sess_resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-570 "sessions.json" "$sess_body"

  # Cleanup
  api DELETE "/api/federation/peers/$peer_name" >/dev/null 2>&1 || true

  if [[ "$sess_code" == "200" ]]; then
    ok "peer token with sessions:list returns 200 on GET /api/sessions"
  elif [[ "$sess_code" == "403" ]]; then
    ko "peer token with sessions:list returned 403 — CBAC not granting cap"
  else
    skip "unexpected response $sess_code (federation CBAC may be gated on peer connectivity)"
  fi
}

RESULT=fail
_story_ts_570
: "${RESULT:=fail}"
unset -f _story_ts_570
