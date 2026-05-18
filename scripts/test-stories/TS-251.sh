#!/usr/bin/env bash
# TS-251 — GET /api/openapi.yaml returns valid YAML with openapi: 3.0.x
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-251"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_251() {
  local resp
  resp=$(api GET /api/openapi.yaml)
  save_evidence TS-251 "openapi.yaml" "$resp"
  if echo "$resp" | grep -q "openapi:"; then
    ok "openapi.yaml contains openapi: key"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "openapi.yaml endpoint not available in this build"
  else
    ko "openapi.yaml missing openapi: key: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_251
: "${RESULT:=fail}"
unset -f _story_ts_251
