#!/usr/bin/env bash
# TS-612 — docker-network routing — node add creates Docker network
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-612"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_612() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r612-dn-node","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r612-net","image":"ollama/ollama:latest","container_name":"r612-ctr","port":11434,"auto_start":true}}'
  api DELETE /api/compute/nodes/r612-dn-node >/dev/null 2>&1 || true
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-612 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST docker-network node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup compute_node "r612-dn-node"

  sleep 4

  if docker network inspect r612-net &>/dev/null 2>&1; then
    ok "Docker network r612-net created successfully"
  else
    ko "Docker network r612-net not found after node creation"
  fi

  api DELETE /api/compute/nodes/r612-dn-node >/dev/null 2>&1
}

RESULT=fail
_story_ts_612
: "${RESULT:=fail}"
unset -f _story_ts_612
