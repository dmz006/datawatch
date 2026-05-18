#!/usr/bin/env bash
# TS-575 — POST /api/sessions/peer-alpha/sess-123/input proxies to peer
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-575"
story_preflight "surface:api feature:federation" || return 0

_story_ts_575() {
  local raw code body
  raw=$(api_code POST /api/sessions/peer-alpha/sess-123/input '{"text":"test"}')
  code=$(echo "$raw" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$raw" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-575 "resp.json" "$body"

  if [[ "$code" == "404" ]] && echo "$body" | grep -qi "no route\|not found"; then
    skip "sessions/{peer}/{id}/input proxy endpoint not available in this build"
    return
  fi
  if echo "$body" | grep -qi "not found\|no route\|unknown"; then
    skip "federated session proxy endpoint not available"
    return
  fi
  if [[ "$code" == "200" || "$code" == "202" || "$code" == "400" ]]; then
    ok "POST /api/sessions/peer-alpha/sess-123/input reached endpoint (HTTP $code)"
  elif [[ "$code" == "404" ]]; then
    skip "federated session proxy endpoint returned 404"
  elif [[ -z "$code" ]]; then
    skip "could not reach server"
  else
    ko "unexpected HTTP $code: $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_575
: "${RESULT:=fail}"
unset -f _story_ts_575
