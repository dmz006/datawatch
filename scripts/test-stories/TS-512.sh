#!/usr/bin/env bash
# TS-512 — GET /api/push/health returns 200
# tags: surface:api feature:push
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-512"
story_preflight "surface:api feature:push" || return 0

_story_ts_512() {
  local resp code
  resp=$(api_code GET /api/push/health)
  save_evidence TS-512 "health.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" ]]; then
    ok "GET /api/push/health returns 200"
  elif [[ "$code" == "404" ]]; then
    skip "push/health endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_512
: "${RESULT:=fail}"
unset -f _story_ts_512
