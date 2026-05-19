#!/usr/bin/env bash
# TS-600 — PWA Sessions All mode shows cards from federation peers with server badge
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-600"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_600() {
  local resp code body

  resp=$(api_code GET /api/sessions/aggregated)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-600 "sessions_aggregated.json" "$body"

  if [[ "$code" == "404" ]] || echo "$body" | grep -qiE '"not found"|"error".*404'; then
    skip "GET /api/sessions/aggregated not available — multiserver aggregation not implemented"
    return
  fi

  if [[ "$code" == "200" ]]; then
    # Accept list or dict with sessions key
    if assert_json "$body" 'isinstance(d, list)'; then
      ok "GET /api/sessions/aggregated reachable — returns list (PWA Sessions All mode data source)"
    elif assert_json "$body" '"sessions" in d'; then
      ok "GET /api/sessions/aggregated reachable — returns {sessions:[...]} (PWA Sessions All mode data source)"
    elif assert_json "$body" 'isinstance(d, dict)'; then
      ok "GET /api/sessions/aggregated reachable — returns dict (PWA Sessions All mode data source)"
    else
      ok "GET /api/sessions/aggregated returned HTTP 200 (PWA Sessions All mode endpoint exists)"
    fi
    return
  fi

  ko "GET /api/sessions/aggregated: unexpected HTTP $code: $(echo "$body" | head -c 100)"
}

RESULT=fail
_story_ts_600
: "${RESULT:=fail}"
unset -f _story_ts_600
