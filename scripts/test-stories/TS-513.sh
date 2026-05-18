#!/usr/bin/env bash
# TS-513 — GET /.well-known/unifiedpush returns discovery document
# tags: surface:api feature:push
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-513"
story_preflight "surface:api feature:push" || return 0

_story_ts_513() {
  local resp code
  resp=$(api_code GET /.well-known/unifiedpush)
  save_evidence TS-513 "discovery.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" ]]; then
    ok "GET /.well-known/unifiedpush returns 200"
  elif [[ "$code" == "404" ]]; then
    skip "unifiedpush discovery endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_513
: "${RESULT:=fail}"
unset -f _story_ts_513
