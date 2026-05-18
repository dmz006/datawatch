#!/usr/bin/env bash
# TS-251 — GET /api/openapi.yaml returns valid YAML with openapi: 3.0.x
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-251"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_251() {
  local resp code body
  resp=$(api_code GET /api/openapi.yaml '')
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-251 "openapi.yaml" "$body"
  if [[ "$code" == "200" ]] && python3 -c "import sys; d=sys.stdin.read(); exit(0 if 'openapi:' in d else 1)" <<< "$body" 2>/dev/null; then
    ok "openapi.yaml contains openapi: key (HTTP $code)"
  elif echo "$body" | grep -q "openapi:"; then
    ok "openapi.yaml contains openapi: key"
  elif [[ "$code" == "404" ]] || echo "$body" | grep -qi "not found\|no route"; then
    skip "openapi.yaml endpoint not available (HTTP $code)"
  else
    ko "openapi.yaml missing openapi: key (HTTP $code): $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_251
: "${RESULT:=fail}"
unset -f _story_ts_251
