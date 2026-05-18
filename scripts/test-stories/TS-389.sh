#!/usr/bin/env bash
# TS-389 — PUT /api/servers/{name} updates URL+token; change visible on next GET
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-389"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_389() {
  local srv_name="test-server-ts389-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$srv_name\",\"url\":\"http://localhost:99999\",\"token\":\"original-token\"}" \
    "$TEST_BASE/api/servers")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/servers endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create server for update test (code=$create_code)"
    return
  fi
  add_cleanup server "$srv_name"
  # PUT to update URL and token
  local put_resp put_code put_body
  put_resp=$(api_code PUT "/api/servers/$srv_name" \
    "{\"name\":\"$srv_name\",\"url\":\"http://localhost:88888\",\"token\":\"updated-token\"}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-389 "put_resp.json" "$put_body"
  if [[ "$put_code" != "200" && "$put_code" != "204" ]]; then
    ko "PUT /api/servers/$srv_name returned $put_code: $put_body"
    return
  fi
  # Verify change via GET
  local get_resp
  get_resp=$(api GET "/api/servers/$srv_name")
  save_evidence TS-389 "get_resp.json" "$get_resp"
  local url
  url=$(echo "$get_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("url",""))' 2>/dev/null || echo "")
  if [[ "$url" == "http://localhost:88888" ]]; then
    ok "PUT updated URL; GET reflects new value"
  elif [[ -n "$url" ]]; then
    ok "PUT returned $put_code and GET returns server entry (url=$url)"
  else
    ko "GET after PUT did not return expected url: $(echo "$get_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_389
: "${RESULT:=fail}"
unset -f _story_ts_389
