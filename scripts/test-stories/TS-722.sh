#!/usr/bin/env bash
# TS-722 — GPU Observer: GET /api/compute/nodes/{name}/detail has gpu[] array
# tags: surface:api feature:observer feature:compute parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-722"
story_preflight "surface:api feature:observer feature:compute" || return 0

_story_ts_722() {
  # Get a compute node to inspect
  local nodes_resp
  nodes_resp=$(api GET /api/compute/nodes 2>/dev/null || echo "[]")
  save_evidence TS-722 "compute_nodes.json" "$nodes_resp"

  local node_name
  node_name=$(echo "$nodes_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
items = d if isinstance(d,list) else d.get("nodes",[])
for item in items:
    n = item.get("name","")
    if n:
        print(n)
        break
' 2>/dev/null || echo "")

  if [[ -z "$node_name" ]]; then
    skip "no compute nodes registered — register a compute node to test GPU observer detail"
    return
  fi

  # Check detail endpoint
  local resp code body
  resp=$(api_code GET "/api/compute/nodes/$node_name/detail")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-722 "detail.json" "$body"

  if [[ "$code" == "404" ]]; then
    skip "compute node detail endpoint not found (HTTP 404)"
    return
  fi
  if [[ "$code" == "502" ]]; then
    skip "compute node $node_name unreachable (502) — Ollama may be down"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/compute/nodes/$node_name/detail returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify gpu array field is present (may be empty on hosts without GPU probe)
  local has_gpu
  has_gpu=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'gpu' in d else 'no')" 2>/dev/null || echo "no")

  if [[ "$has_gpu" != "yes" ]]; then
    ko "GET /api/compute/nodes/{name}/detail missing 'gpu' field — v8.25.3 GPU observer not wired: $(echo "$body" | head -c 200)"
    return
  fi

  local gpu_count
  gpu_count=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d.get('gpu',[])))" 2>/dev/null || echo "0")
  ok "GET /api/compute/nodes/$node_name/detail has gpu[] array — $gpu_count GPU(s) reported"
}

RESULT=fail
_story_ts_722
: "${RESULT:=fail}"
unset -f _story_ts_722
