#!/usr/bin/env bash
# TS-411 — DELETE /api/compute/nodes/{name} returns 200; GET returns 404
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-411"
story_preflight "surface:api feature:compute" || return 0

_story_ts_411() {
  local node_name="test-compute-ts411-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$node_name\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}" \
    "$TEST_BASE/api/compute/nodes?probe=skip")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/compute/nodes endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create compute node for delete test (code=$create_code)"
    return
  fi
  # DELETE it
  local del_code
  del_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X DELETE -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/compute/nodes/$node_name")
  save_evidence TS-411 "del_code.txt" "$del_code"
  if [[ "$del_code" != "200" && "$del_code" != "204" ]]; then
    ko "DELETE /api/compute/nodes/$node_name returned $del_code (expected 200/204)"
    return
  fi
  # GET should return 404
  local get_code
  get_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/compute/nodes/$node_name")
  save_evidence TS-411 "get_after_delete.txt" "$get_code"
  if [[ "$get_code" == "404" ]]; then
    ok "DELETE returned $del_code; subsequent GET returns 404"
  else
    ko "DELETE returned $del_code but GET returned $get_code (expected 404)"
  fi
}

RESULT=fail
_story_ts_411
: "${RESULT:=fail}"
unset -f _story_ts_411
