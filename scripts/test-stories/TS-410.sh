#!/usr/bin/env bash
# TS-410 — POST /api/compute/nodes creates entry, GET /api/compute/nodes returns it
# tags: surface:api feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-410"
story_preflight "surface:api feature:compute" || return 0

_story_ts_410() {
  local node_name="test-compute-ts410-$$"
  local resp code body
  resp=$(api_code POST "/api/compute/nodes?probe=skip" \
    "{\"name\":\"$node_name\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-410 "create_resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/compute/nodes endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST /api/compute/nodes returned $code: $body"
    return
  fi
  add_cleanup compute_node "$node_name"
  # GET list and check our node is present
  local list_resp
  list_resp=$(api GET /api/compute/nodes)
  save_evidence TS-410 "list_resp.json" "$list_resp"
  if echo "$list_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('nodes',[])
assert any(n.get('name')=='$node_name' for n in items)
" 2>/dev/null; then
    ok "POST /api/compute/nodes created entry; GET /api/compute/nodes returns it"
  else
    ko "node $node_name not found in list after creation: $(echo "$list_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_410
: "${RESULT:=fail}"
unset -f _story_ts_410
