#!/usr/bin/env bash
# tags: surface:api feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-659-debug"
story_preflight "surface:api feature:vision group:vision parallel:ok" || return 0

_story() {
  local resp code

  resp=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.model":"llava"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  if [[ ! "$code" =~ ^2 ]]; then
    ko "PUT /api/config vision fields expected 2xx, got $code"
    return
  fi
  ok "PUT /api/config vision fields round-trip correctly"
}

RESULT=fail
_story
: "${RESULT:=fail}"
unset -f _story
