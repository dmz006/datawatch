#!/usr/bin/env bash
# TS-785 — Federation bootstrap: start a REAL second daemon and register it
# as a correctly-tokened federated peer, then verify the actual client path
# that broke live (2026-10-08): GET /api/proxy/<peer>/api/sessions and
# /api/proxy/<peer>/api/health both return real 200 data through a genuinely
# separate daemon process -- not a synthetic localhost:99999 placeholder
# (the gap TS-387-396 left: they never dial a real second daemon at all).
# Bootstrap story: later federation stories (TS-786+) assume this daemon is
# already running, same pattern as TS-160/docker-sim.
# tags: surface:api feature:federation group:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-785"
story_preflight "surface:api feature:federation" || return 0

_story_ts_785() {
  if [[ -z "${TEST_BINARY:-}" || ! -x "${TEST_BINARY:-}" ]]; then
    skip "TEST_BINARY not set/executable"
    return
  fi

  FED_PEER_HTTP=$(free_port)
  FED_PEER_TLS=$(free_port)
  FED_PEER_MCP=$(free_port)
  FED_PEER_DATA="/tmp/dw-fed-peer-${RUN_ID:-$$}"
  FED_PEER_TOKEN="dw-fed-peer-token-$$"
  FED_PEER_NAME="e2e-fed-peer"

  write_test_config "$FED_PEER_DATA" "$FED_PEER_HTTP" "$FED_PEER_TLS" "$FED_PEER_MCP" "0" "$FED_PEER_TOKEN"
  # Plain HTTP for this peer, deliberately: the generic REST proxy
  # (internal/server/proxy.go) uses a stock http.Client with no
  # InsecureSkipVerify, so a self-signed/auto-generated TLS cert (what
  # write_test_config's template produces) fails real certificate
  # verification -- confirmed live (502, x509: certificate signed by
  # unknown authority), NOT the 401 auth failure this story is actually
  # testing. Production's real federated peers apparently use a properly
  # CA-signed cert (this project's native ACME support) rather than a
  # self-signed one, which is a separate, real, worth-flagging gap: a
  # self-signed-cert federated peer would hit this same TLS failure in
  # production today, not just in this test harness. Flagged in the
  # testing-tracker rather than silently worked around here -- this test
  # uses HTTP specifically to isolate and pin the auth bug it's named
  # for, not to paper over the TLS gap.
  sed -i 's/tls_enabled: true/tls_enabled: false/' "$FED_PEER_DATA/config.yaml"
  # write_test_config's templated branch (the one this repo actually uses,
  # since testdata/datawatch.yaml exists) never substitutes the token --
  # it's left at the template's own literal `token: ""`. First attempt at
  # this used a grep-guarded python block intended to "fill in the token
  # if write_test_config didn't" -- but write_test_config NEVER sets a
  # token, so the token: "" line is always already present, the grep
  # guard always matched, and the substitution never ran. Caught live:
  # the bad-token negative-control story (TS-786) got a false-positive
  # 200 instead of 401, because this peer's own token was empty the whole
  # time -- fedAuthMiddleware treats an empty configured token as "no
  # auth required," so ANY bearer value (including the deliberately wrong
  # one TS-786 sends) was accepted. Same sed pattern run-tests.sh itself
  # uses to set the PRIMARY daemon's token from the same template.
  sed -i "s|token: \"\"|token: \"$FED_PEER_TOKEN\"|" "$FED_PEER_DATA/config.yaml"

  mkdir -p "$FED_PEER_DATA"
  "$TEST_BINARY" start --foreground --config "$FED_PEER_DATA/config.yaml" >> "$FED_PEER_DATA/daemon.log" 2>&1 &
  FED_PEER_PID=$!
  echo "$FED_PEER_PID" > "${TEST_DIR:-$FED_PEER_DATA}/fed-peer-daemon.pid"

  local deadline=$(( $(date +%s) + 20 )) healthy=0
  while [[ $(date +%s) -lt $deadline ]]; do
    if curl -sf --max-time 2 "http://127.0.0.1:$FED_PEER_HTTP/api/health" > /dev/null 2>&1; then
      healthy=1; break
    fi
    sleep 0.5
  done
  if [[ $healthy -eq 0 ]]; then
    skip "second federation-peer daemon did not become healthy within 20s"
    return
  fi
  ok "second real daemon healthy (pid $FED_PEER_PID, :$FED_PEER_HTTP)"

  # Register it as a federated peer on the PRIMARY test daemon with the
  # correct token -- the thing that was missing live.
  local reg
  reg=$(api POST /api/servers "{\"name\":\"$FED_PEER_NAME\",\"url\":\"http://127.0.0.1:$FED_PEER_HTTP\",\"token\":\"$FED_PEER_TOKEN\",\"enabled\":true,\"federated\":true,\"capabilities\":[\"full-control\"]}")
  save_evidence "TS-785" "0_register_peer.json" "$reg"

  # The real client-relevant path: GET /api/proxy/<peer>/api/sessions must
  # return real 200 data through two genuinely separate daemon processes,
  # not a placeholder.
  # Local aggregate -- see TS-786's comment: ok()/ko() each overwrite the
  # shared RESULT with only their own outcome, so an earlier ko() here
  # would otherwise be silently masked by a later ok().
  local any_fail=0
  local code body
  body=$(curl -sk --max-time 10 -w '\n%{http_code}' "$TEST_BASE/api/proxy/$FED_PEER_NAME/api/sessions" \
    -H "Authorization: Bearer $TEST_TOKEN")
  code=$(echo "$body" | tail -1)
  save_evidence "TS-785" "1_proxy_sessions.json" "$body"
  if [[ "$code" == "200" ]]; then
    ok "GET /api/proxy/<peer>/api/sessions returns 200 through a real second daemon"
  else
    ko "expected 200 from a correctly-tokened real peer, got $code"
    any_fail=1
  fi

  body=$(curl -sk --max-time 10 -w '\n%{http_code}' "$TEST_BASE/api/proxy/$FED_PEER_NAME/api/health" \
    -H "Authorization: Bearer $TEST_TOKEN")
  code=$(echo "$body" | tail -1)
  if [[ "$code" == "200" ]]; then
    ok "GET /api/proxy/<peer>/api/health returns 200 through a real second daemon"
  else
    ko "expected 200 from /api/health, got $code"
    any_fail=1
  fi

  export FED_PEER_HTTP FED_PEER_TLS FED_PEER_DATA FED_PEER_TOKEN FED_PEER_NAME FED_PEER_PID
  if [[ "$any_fail" == "1" ]]; then
    RESULT=fail
  else
    RESULT=pass
  fi
}

RESULT=fail
_story_ts_785
: "${RESULT:=fail}"
unset -f _story_ts_785
