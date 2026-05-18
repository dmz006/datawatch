#!/usr/bin/env bash
# TS-387 — POST /api/servers creates entry, GET /api/servers returns it
# tags: surface:api feature:multi-server
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-387"
story_preflight "surface:api feature:multi-server" || return 0

_story_ts_387() {
  local srv_name="test-server-ts387-$$"
  local resp code body
  resp=$(api_code POST /api/servers \
    "{\"name\":\"$srv_name\",\"url\":\"http://localhost:99999\",\"token\":\"test\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-387 "create_resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/servers endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST /api/servers returned $code: $body"
    return
  fi
  add_cleanup server "$srv_name"
  # GET list and check our server is present
  local list_resp
  list_resp=$(api GET /api/servers)
  save_evidence TS-387 "list_resp.json" "$list_resp"
  if echo "$list_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); items=d if isinstance(d,list) else d.get('servers',[]); assert any(s.get('name')=='$srv_name' for s in items)" 2>/dev/null; then
    ok "POST /api/servers created entry; GET /api/servers returns it"
  else
    ko "server $srv_name not found in list after creation: $(echo "$list_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_387
: "${RESULT:=fail}"
unset -f _story_ts_387
