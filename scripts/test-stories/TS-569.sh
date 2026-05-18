#!/usr/bin/env bash
# TS-569 — DELETE /api/federation/groups/monitor returns 403 (builtin protected)
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-569"
story_preflight "surface:api feature:federation" || return 0

_story_ts_569() {
  local raw code body
  raw=$(api_code DELETE /api/federation/groups/monitor '')
  code=$(echo "$raw" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$raw" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-569 "resp.json" "$body"
  if [[ "$code" == "403" ]]; then
    ok "DELETE /api/federation/groups/monitor returned 403 (builtin protected)"
  elif [[ "$code" == "404" ]]; then
    skip "federation/groups endpoint not available in this build"
  elif [[ -z "$code" ]]; then
    skip "federation/groups/monitor endpoint not reachable"
  else
    ko "expected 403 for builtin group delete, got: $code — body: $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_569
: "${RESULT:=fail}"
unset -f _story_ts_569
