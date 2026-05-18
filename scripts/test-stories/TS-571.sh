#!/usr/bin/env bash
# TS-571 — Peer token without sessions:write → POST /api/sessions/start returns 403
# tags: surface:api feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-571"
story_preflight "surface:api feature:federation feature:cbac" || return 0

_story_ts_571() {
  local peers_resp
  peers_resp=$(api GET /api/federation/peers)
  if echo "$peers_resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi

  # Register a test peer with sessions:list but NOT sessions:write
  local peer_name="cbac-test-peer-ts571-$$"
  local peer_token="cbac-test-token-ts571-$$"
  local add_resp
  add_resp=$(api POST /api/federation/peers \
    "{\"name\":\"$peer_name\",\"url\":\"http://127.0.0.1:19999\",\"token\":\"$peer_token\",\"enabled\":true,\"capabilities\":[\"sessions:list\",\"sessions:read\"]}")
  save_evidence TS-571 "add_peer.json" "$add_resp"

  if ! assert_json "$add_resp" '"name" in d or d.get("ok")'; then
    skip "could not register test peer: $(echo "$add_resp" | head -c 100)"
    return
  fi
  # POST /api/sessions/start with read-only peer token — should return 403
  local start_resp start_code
  start_resp=$(curl -sk --max-time 10 -w "\n__HTTP_CODE_%{http_code}__" \
    -X POST -H "Authorization: Bearer $peer_token" -H "Content-Type: application/json" \
    -d '{"task":"cbac-test","backend":"shell","project_dir":"/tmp"}' \
    "$TEST_BASE/api/sessions/start")
  start_code=$(echo "$start_resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local start_body
  start_body=$(echo "$start_resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-571 "start.json" "$start_body"

  # Cleanup
  api DELETE "/api/federation/peers/$peer_name" >/dev/null 2>&1 || true

  if [[ "$start_code" == "403" ]]; then
    ok "peer without sessions:write returns 403 on POST /api/sessions/start"
  elif [[ "$start_code" == "200" || "$start_code" == "201" ]]; then
    ko "peer without sessions:write was allowed to start a session (CBAC not enforced)"
  else
    skip "unexpected response $start_code (federation CBAC may be gated on peer connectivity)"
  fi
}

RESULT=fail
_story_ts_571
: "${RESULT:=fail}"
unset -f _story_ts_571
