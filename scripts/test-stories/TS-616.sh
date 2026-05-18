#!/usr/bin/env bash
# TS-616 — docker-network routing — DELETE removes container
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-616"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_616() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r616-dn-node","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r616-net","image":"ollama/ollama:latest","container_name":"r616-ctr","port":11434,"auto_start":true}}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-616 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST docker-network node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi

  sleep 3

  api DELETE /api/compute/nodes/r616-dn-node >/dev/null 2>&1

  sleep 2

  local remaining
  remaining=$(docker ps -a --filter name=r616-ctr --format "{{.Names}}" 2>/dev/null || echo "")
  save_evidence TS-616 "docker_ps_after_delete.txt" "$remaining"

  if ! echo "$remaining" | grep -q "r616-ctr"; then
    ok "container r616-ctr removed after node DELETE"
  else
    ko "container r616-ctr still present after node DELETE"
  fi
}

RESULT=fail
_story_ts_616
: "${RESULT:=fail}"
unset -f _story_ts_616
