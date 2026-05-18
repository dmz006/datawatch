#!/usr/bin/env bash
# TS-621 — v8.0 smoke — compute node REST CRUD includes routing field
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-621"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_621() {
  # POST
  api DELETE /api/compute/nodes/r621-smoke >/dev/null 2>&1 || true
  local payload resp code
  payload='{"name":"r621-smoke","kind":"ollama","address":"http://localhost:11434","routing":"direct"}'
  resp=$(api_code POST "/api/compute/nodes?probe=skip" "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-621 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST compute node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup compute_node "r621-smoke"

  # GET — verify routing field
  local get_resp
  get_resp=$(api GET /api/compute/nodes/r621-smoke)
  save_evidence TS-621 "get.json" "$get_resp"

  if ! echo "$get_resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "routing" in d' 2>/dev/null; then
    ko "routing field missing from GET response: $(echo "$get_resp" | head -c 200)"
    api DELETE /api/compute/nodes/r621-smoke >/dev/null 2>&1
    return
  fi

  # PUT — update declared_capacity
  local put_resp put_code
  put_resp=$(api_code PUT /api/compute/nodes/r621-smoke '{"declared_capacity":{"max_concurrent_models":2}}')
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-621 "put.json" "$put_resp"

  if [[ "$put_code" != "200" && "$put_code" != "204" ]]; then
    ko "PUT compute node expected 200/204, got $put_code"
    api DELETE /api/compute/nodes/r621-smoke >/dev/null 2>&1
    return
  fi

  # DELETE
  api DELETE /api/compute/nodes/r621-smoke >/dev/null 2>&1
  ok "compute node CRUD with routing field: POST/GET/PUT/DELETE all successful"
}

RESULT=fail
_story_ts_621
: "${RESULT:=fail}"
unset -f _story_ts_621
