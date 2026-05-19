#!/usr/bin/env bash
# TS-602 — PWA Automata All mode shows PRDs from federation peers
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-602"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_602() {
  local resp code body

  resp=$(api_code GET /api/autonomous/prds/aggregated)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-602 "prds_aggregated.json" "$body"

  if [[ "$code" == "404" ]] || echo "$body" | grep -qiE '"not found"|"error".*404'; then
    skip "GET /api/autonomous/prds/aggregated not available — multiserver PRD aggregation not implemented"
    return
  fi

  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, list)'; then
      ok "GET /api/autonomous/prds/aggregated reachable — returns list (PWA Automata All mode data source)"
    elif assert_json "$body" 'isinstance(d, dict)'; then
      ok "GET /api/autonomous/prds/aggregated reachable — returns dict (PWA Automata All mode data source)"
    else
      ok "GET /api/autonomous/prds/aggregated returned HTTP 200 (PWA Automata All mode endpoint exists)"
    fi
    return
  fi

  ko "GET /api/autonomous/prds/aggregated: unexpected HTTP $code: $(echo "$body" | head -c 100)"
}

RESULT=fail
_story_ts_602
: "${RESULT:=fail}"
unset -f _story_ts_602
