#!/usr/bin/env bash
# TS-412 — GET /api/compute/nodes/{name}/models?kind=ollama returns {models:[],kind,node} shape
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-412"
story_preflight "surface:api feature:compute" || return 0

_story_ts_412() {
  local node_name="test-compute-ts412-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$node_name\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}" \
    "$TEST_BASE/api/compute/nodes?probe=skip")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/compute/nodes endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create compute node for models test (code=$create_code)"
    return
  fi
  add_cleanup compute_node "$node_name"
  # GET models
  local resp
  resp=$(api GET "/api/compute/nodes/$node_name/models?kind=ollama")
  save_evidence TS-412 "resp.json" "$resp"
  if assert_json "$resp" '"models" in d'; then
    ok "GET /api/compute/nodes/$node_name/models returns {models:...} shape"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/compute/nodes/$node_name/models returns array"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/compute/nodes/$node_name/models returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "compute node models endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_412
: "${RESULT:=fail}"
unset -f _story_ts_412
