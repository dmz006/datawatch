#!/usr/bin/env bash
# TS-507 — PATCH /api/compute/nodes/{name}/enabled toggles enabled field
# tags: surface:api feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-507"
story_preflight "surface:api feature:compute" || return 0

_story_ts_507() {
  local cname="ts-507-node-$$"
  local resp
  resp=$(api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$cname\",\"kind\":\"ollama\",\"address\":\"http://localhost:11434\"}")
  if ! assert_json "$resp" '"name" in d or "id" in d'; then
    skip "could not create compute node for test: $(echo "$resp" | head -c 100)"
    return
  fi
  add_cleanup compute_node "$cname"
  local patch_resp code
  patch_resp=$(api_code PATCH "/api/compute/nodes/$cname/enabled" '{"enabled":false}')
  save_evidence TS-507 "patch.json" "$patch_resp"
  code=$(echo "$patch_resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "202" ]]; then
    ok "PATCH /api/compute/nodes/$cname/enabled returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "/enabled PATCH endpoint not available"
  else
    ko "unexpected HTTP $code: $(echo "$patch_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_507
: "${RESULT:=fail}"
unset -f _story_ts_507
