#!/usr/bin/env bash
# TS-611 — docker-network routing — missing image field returns 400
# tags: surface:api feature:routing group:routing-v8 parallel:ok conflict:docker
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-611"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok conflict:docker" || return 0

_story_ts_611() {
  if ! command -v docker &>/dev/null || ! docker info &>/dev/null 2>&1; then
    skip "docker not available"; return
  fi

  local payload resp code
  payload='{"name":"r611-dn-noimgae","kind":"ollama","address":"http://localhost:11434","routing":"docker-network","routing_docker_network":{"network":"r611-net","container_name":"r611-ctr","port":11434}}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-611 "create_no_image.json" "$resp"

  if [[ "$code" == "400" ]]; then
    ok "docker-network node without image field correctly rejected with 400"
  else
    ko "expected 400 for missing image field, got $code"
  fi
}

RESULT=fail
_story_ts_611
: "${RESULT:=fail}"
unset -f _story_ts_611
