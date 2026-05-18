#!/usr/bin/env bash
# TS-609 — Direct routing — POST node with routing:direct, verify routing field, DELETE
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-609"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_609() {
  local payload resp code node_id
  payload='{"name":"r609-node","kind":"ollama","address":"http://localhost:11434","routing":"direct"}'
  api DELETE /api/compute/nodes/r609-node >/dev/null 2>&1 || true
  resp=$(api_code POST "/api/compute/nodes?probe=skip" "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-609 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST compute node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi

  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  node_id=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id","") or d.get("name",""))' 2>/dev/null || echo "r609-node")
  add_cleanup compute_node "r609-node"

  local get_resp
  get_resp=$(api GET /api/compute/nodes/r609-node)
  save_evidence TS-609 "get.json" "$get_resp"

  if echo "$get_resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "routing" in d' 2>/dev/null; then
    ok "routing field present in GET response"
  else
    ko "routing field missing from GET response: $(echo "$get_resp" | head -c 200)"
    api DELETE /api/compute/nodes/r609-node >/dev/null 2>&1
    return
  fi

  api DELETE /api/compute/nodes/r609-node >/dev/null 2>&1
  ok "TS-609 direct routing node created, verified, and deleted"
}

RESULT=fail
_story_ts_609
: "${RESULT:=fail}"
unset -f _story_ts_609
