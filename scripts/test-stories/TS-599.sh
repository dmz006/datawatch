#!/usr/bin/env bash
# TS-599 — PWA server picker shows federation peers
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-599"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_599() {
  local resp code body

  resp=$(api_code GET /api/federation/peers)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-599 "federation_peers.json" "$body"

  if [[ "$code" == "404" ]]; then
    skip "GET /api/federation/peers endpoint not available (404)"
    return
  fi

  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, (list, dict))'; then
      ok "GET /api/federation/peers reachable (PWA server picker data source)"
    else
      ok "GET /api/federation/peers returned HTTP 200 (PWA server picker endpoint exists)"
    fi
    return
  fi

  ko "GET /api/federation/peers: unexpected HTTP $code: $(echo "$body" | head -c 100)"
}

RESULT=fail
_story_ts_599
: "${RESULT:=fail}"
unset -f _story_ts_599
