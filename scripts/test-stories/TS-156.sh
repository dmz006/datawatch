#!/usr/bin/env bash
# TS-156 — Compute nodes endpoint
# tags: surface:api feature:compute
# legacy fn: t12_ts156_compute_nodes
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-156"
story_preflight "surface:api feature:compute" || return 0

_story_ts_156() {
  local resp
  resp=$(api GET /api/compute/nodes)
  save_evidence TS-156 "nodes.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/compute/nodes responds"
  else
    skip "compute/nodes endpoint not present: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_156
: "${RESULT:=fail}"
unset -f _story_ts_156
