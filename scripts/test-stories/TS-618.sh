#!/usr/bin/env bash
# TS-618 — datawatch-proxy routing — proxy node added against registered peer
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-618"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_618() {
  # Register a peer server (use self as peer for test)
  api DELETE /api/servers/r618-peer >/dev/null 2>&1 || true
  api DELETE /api/compute/nodes/r618-proxy-node >/dev/null 2>&1 || true
  local peer_payload peer_resp peer_code
  peer_payload='{"name":"r618-peer","url":"'"$TEST_BASE"'","token":"'"$TEST_TOKEN"'"}'
  peer_resp=$(api_code POST /api/servers "$peer_payload")
  peer_code=$(echo "$peer_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-618 "peer_create.json" "$peer_resp"

  if [[ "$peer_code" != "200" && "$peer_code" != "201" ]]; then
    ko "POST /api/servers expected 200/201, got $peer_code: $(echo "$peer_resp" | head -c 200)"
    return
  fi
  add_cleanup server "r618-peer"

  # POST proxy node
  local node_payload node_resp node_code
  node_payload='{"name":"r618-proxy-node","kind":"ollama","address":"http://localhost:11434","routing":"datawatch-proxy","routing_datawatch_proxy":{"peer":"r618-peer","remote_llm_name":"test-llm"}}'
  node_resp=$(api_code POST /api/compute/nodes "$node_payload")
  node_code=$(echo "$node_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-618 "node_create.json" "$node_resp"

  if [[ "$node_code" == "200" || "$node_code" == "201" ]]; then
    ok "datawatch-proxy node created successfully against registered peer"
    add_cleanup compute_node "r618-proxy-node"
    api DELETE /api/compute/nodes/r618-proxy-node >/dev/null 2>&1
  else
    ko "POST proxy node expected 200/201, got $node_code: $(echo "$node_resp" | head -c 200)"
  fi

  api DELETE /api/servers/r618-peer >/dev/null 2>&1
}

RESULT=fail
_story_ts_618
: "${RESULT:=fail}"
unset -f _story_ts_618
