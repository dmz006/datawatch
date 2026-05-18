#!/usr/bin/env bash
# TS-615 — docker-network routing — GET /detail returns container_running
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-615"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_615() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r615-dn-node","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r615-net","image":"ollama/ollama:latest","container_name":"r615-ctr","port":11434,"auto_start":true}}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-615 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST docker-network node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup compute_node "r615-dn-node"

  sleep 3

  local detail
  detail=$(curl "${curl_args[@]}" -o /tmp/r615_detail.json -w "%{http_code}" "$TEST_BASE/api/compute/nodes/r615-dn-node/detail" 2>/dev/null)
  local detail_body
  detail_body=$(cat /tmp/r615_detail.json 2>/dev/null || echo "{}")
  save_evidence TS-615 "detail.json" "$detail_body"

  if [[ "$detail" != "200" ]]; then
    ko "GET /detail expected 200, got $detail"
    api DELETE /api/compute/nodes/r615-dn-node >/dev/null 2>&1
    return
  fi

  if echo "$detail_body" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "container_running" in d' 2>/dev/null; then
    ok "GET /detail returns container_running field"
  else
    ko "container_running field missing from /detail response: $(echo "$detail_body" | head -c 200)"
  fi

  api DELETE /api/compute/nodes/r615-dn-node >/dev/null 2>&1
}

RESULT=fail
_story_ts_615
: "${RESULT:=fail}"
unset -f _story_ts_615
