#!/usr/bin/env bash
# TS-390 — DELETE /api/servers/{name} returns 200; GET returns 404
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-390"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_390() {
  local srv_name="test-server-ts390-$$"
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
    ko "could not create server for delete test (code=$create_code)"
    return
  fi
  # DELETE it
  local del_code
  del_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X DELETE -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/servers/$srv_name")
  save_evidence TS-390 "del_code.txt" "$del_code"
  if [[ "$del_code" != "200" && "$del_code" != "204" ]]; then
    ko "DELETE /api/servers/$srv_name returned $del_code (expected 200/204)"
    return
  fi
  # GET should now return 404
  local get_code
  get_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/servers/$srv_name")
  save_evidence TS-390 "get_after_delete.txt" "$get_code"
  if [[ "$get_code" == "404" ]]; then
    ok "DELETE returned $del_code; subsequent GET returns 404"
  else
    ko "DELETE returned $del_code but GET returned $get_code (expected 404)"
  fi
}

RESULT=fail
_story_ts_390
: "${RESULT:=fail}"
unset -f _story_ts_390
