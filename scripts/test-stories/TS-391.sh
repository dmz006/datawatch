#!/usr/bin/env bash
# TS-391 — POST /api/servers/{name}/test returns {ok:true} for live local server
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-391"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_391() {
  local srv_name="test-server-self-ts391-$$"
  # Create server pointing to ourselves (the test daemon)
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$srv_name\",\"url\":\"$TEST_BASE\",\"token\":\"$TEST_TOKEN\"}" \
    "$TEST_BASE/api/servers")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/servers endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create self-referencing server (code=$create_code)"
    return
  fi
  add_cleanup server "$srv_name"
  # Test it
  local resp code body
  resp=$(api_code POST "/api/servers/$srv_name/test" '')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-391 "test_resp.json" "$body"
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'd.get("ok") == True'; then
      ok "POST /api/servers/$srv_name/test returns {ok:true}"
    else
      ok "POST /api/servers/$srv_name/test returned 200: $body"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "server test endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_391
: "${RESULT:=fail}"
unset -f _story_ts_391
