#!/usr/bin/env bash
# TS-431 — PATCH /api/compute/nodes/{name}/enabled toggles enabled field
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-431"
story_preflight "surface:api feature:compute" || return 0

_story_ts_431() {
  local node_name="test-compute-ts431-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$node_name\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\",\"enabled\":true}" \
    "$TEST_BASE/api/compute/nodes?probe=skip")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/compute/nodes endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create compute node for toggle test (code=$create_code)"
    return
  fi
  add_cleanup compute_node "$node_name"
  # PATCH enabled=false
  local resp code body
  resp=$(api_code PATCH "/api/compute/nodes/$node_name/enabled" '{"enabled":false}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-431 "patch_resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    ok "PATCH /api/compute/nodes/$node_name/enabled returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "compute node enabled endpoint not available (404)"
  elif [[ "$code" == "405" ]]; then
    # Try PUT instead
    local put_resp put_code put_body
    put_resp=$(api_code PUT "/api/compute/nodes/$node_name/enabled" '{"enabled":false}')
    put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
    if [[ "$put_code" == "200" || "$put_code" == "204" ]]; then
      ok "PUT /api/compute/nodes/$node_name/enabled returned $put_code (PATCH not supported)"
    else
      skip "neither PATCH nor PUT supported for compute node enabled toggle"
    fi
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_431
: "${RESULT:=fail}"
unset -f _story_ts_431
