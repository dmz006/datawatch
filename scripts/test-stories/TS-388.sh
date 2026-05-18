#!/usr/bin/env bash
# TS-388 — GET /api/servers/{name} returns single entry; 404 on unknown
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-388"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_388() {
  local srv_name="test-server-ts388-$$"
  # Create
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$srv_name\",\"url\":\"http://localhost:99999\",\"token\":\"test\"}" \
    "$TEST_BASE/api/servers")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/servers endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create server (create code=$create_code)"
    return
  fi
  add_cleanup server "$srv_name"
  # GET by name
  local get_resp
  get_resp=$(api GET "/api/servers/$srv_name")
  save_evidence TS-388 "get_resp.json" "$get_resp"
  if assert_json "$get_resp" '"name" in d'; then
    ok "GET /api/servers/$srv_name returns entry"
  elif echo "$get_resp" | grep -qi "not found\|404"; then
    ko "GET /api/servers/$srv_name returned 404 after creation"
    return
  else
    ko "unexpected GET response: $(echo "$get_resp" | head -c 200)"
    return
  fi
  # GET unknown name should 404
  local code404
  code404=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/servers/nonexistent-server-ts388-zz999")
  save_evidence TS-388 "code_unknown.txt" "$code404"
  if [[ "$code404" == "404" ]]; then
    ok "GET /api/servers/unknown returns 404"
  else
    ko "GET /api/servers/unknown returned $code404 (expected 404)"
  fi
}

RESULT=fail
_story_ts_388
: "${RESULT:=fail}"
unset -f _story_ts_388
