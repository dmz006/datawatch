#!/usr/bin/env bash
# TS-504 — GET /api/compute/nodes/{name}/detail returns 200 or 503 (never 500)
# tags: surface:api feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-504"
story_preflight "surface:api feature:compute" || return 0

_story_ts_504() {
  local cname="ts-504-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  local detail_resp code
  detail_resp=$(api_code GET "/api/compute/nodes/$cname/detail")
  save_evidence TS-504 "detail.json" "$detail_resp"
  code=$(echo "$detail_resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "500" ]]; then
    ko "GET /api/compute/nodes/$cname/detail returned 500 (internal server error)"
  elif [[ "$code" == "200" || "$code" == "503" || "$code" == "404" ]]; then
    ok "GET /api/compute/nodes/$cname/detail returned $code (not 500)"
  elif echo "$detail_resp" | grep -qi "not found\|__HTTP_CODE_404__"; then
    skip "/detail endpoint not available"
  else
    ko "unexpected HTTP $code: $(echo "$detail_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_504
: "${RESULT:=fail}"
unset -f _story_ts_504
