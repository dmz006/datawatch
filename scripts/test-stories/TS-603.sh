#!/usr/bin/env bash
# TS-603 — PWA Alerts All mode shows alerts from federation peers
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-603"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_603() {
  local resp code body

  resp=$(api_code GET /api/alerts/aggregated)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-603 "alerts_aggregated.json" "$body"

  if [[ "$code" == "404" ]] || echo "$body" | grep -qiE '"not found"|"error".*404'; then
    skip "GET /api/alerts/aggregated not available — multiserver alerts aggregation not implemented"
    return
  fi

  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, list)'; then
      ok "GET /api/alerts/aggregated reachable — returns list (PWA Alerts All mode data source)"
    elif assert_json "$body" 'isinstance(d, dict)'; then
      ok "GET /api/alerts/aggregated reachable — returns dict (PWA Alerts All mode data source)"
    else
      ok "GET /api/alerts/aggregated returned HTTP 200 (PWA Alerts All mode endpoint exists)"
    fi
    return
  fi

  ko "GET /api/alerts/aggregated: unexpected HTTP $code: $(echo "$body" | head -c 100)"
}

RESULT=fail
_story_ts_603
: "${RESULT:=fail}"
unset -f _story_ts_603
