#!/usr/bin/env bash
# TS-614 — docker-network routing — idempotent, no extra container spawned on re-probe
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-614"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_614() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r614-dn-node","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r614-net","image":"ollama/ollama:latest","container_name":"r614-ctr","port":11434,"auto_start":true}}'
  api DELETE /api/compute/nodes/r614-dn-node >/dev/null 2>&1 || true
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-614 "create.json" "$resp"

  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST docker-network node expected 200/201, got $code: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup compute_node "r614-dn-node"

  sleep 3

  local BEFORE
  BEFORE=$(docker ps --filter name=r614-ctr --format "{{.ID}}" 2>/dev/null | wc -l)

  curl "${curl_args[@]}" "$TEST_BASE/api/compute/nodes/r614-dn-node/health" >/dev/null 2>&1

  sleep 1

  local AFTER
  AFTER=$(docker ps --filter name=r614-ctr --format "{{.ID}}" 2>/dev/null | wc -l)
  save_evidence TS-614 "counts.txt" "BEFORE=$BEFORE AFTER=$AFTER"

  if [[ "$AFTER" -le "$BEFORE" ]]; then
    ok "idempotent: no extra container spawned on re-probe (before=$BEFORE after=$AFTER)"
  else
    ko "extra container spawned on re-probe (before=$BEFORE after=$AFTER)"
  fi

  api DELETE /api/compute/nodes/r614-dn-node >/dev/null 2>&1
}

RESULT=fail
_story_ts_614
: "${RESULT:=fail}"
unset -f _story_ts_614
