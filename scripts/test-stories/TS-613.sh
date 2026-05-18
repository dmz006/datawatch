#!/usr/bin/env bash
# TS-613 — docker-network routing — container launched on probe
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-613"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_613() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r613-dn-node","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r613-net","image":"ollama/ollama:latest","container_name":"r613-ctr","port":11434,"auto_start":true}}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-613 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST docker-network node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup compute_node "r613-dn-node"

  sleep 4

  local ps_out
  ps_out=$(docker ps --filter name=r613-ctr --format "{{.Names}}" 2>/dev/null || echo "")
  save_evidence TS-613 "docker_ps.txt" "$ps_out"

  if echo "$ps_out" | grep -q "r613-ctr"; then
    ok "container r613-ctr is running after node probe"
  else
    ko "container r613-ctr not found in docker ps after node creation"
  fi

  api DELETE /api/compute/nodes/r613-dn-node >/dev/null 2>&1
}

RESULT=fail
_story_ts_613
: "${RESULT:=fail}"
unset -f _story_ts_613
