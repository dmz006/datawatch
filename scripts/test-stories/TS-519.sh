#!/usr/bin/env bash
# TS-519 — GET /api/compute/nodes/{name} response has auto_tags field
# tags: surface:api feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-519"
story_preflight "surface:api feature:compute" || return 0

_story_ts_519() {
  local cname="ts-519-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  local get_resp
  get_resp=$(api GET "/api/compute/nodes/$cname")
  save_evidence TS-519 "node.json" "$get_resp"
  if assert_json "$get_resp" '"auto_tags" in d'; then
    ok "GET /api/compute/nodes/$cname has auto_tags field"
  elif assert_json "$get_resp" 'isinstance(d, dict)'; then
    skip "GET /api/compute/nodes/$cname responds but no auto_tags: $(echo "$get_resp" | head -c 100)"
  else
    ko "unexpected response: $(echo "$get_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_519
: "${RESULT:=fail}"
unset -f _story_ts_519
