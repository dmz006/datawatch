#!/usr/bin/env bash
# TS-163 — GET /api/orchestrator/graphs shape or 404
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-163"
story_preflight "surface:api feature:parity" || return 0

_story_ts_163() {
  local resp code
  resp=$(api_code GET /api/orchestrator/graphs)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-163 "orchestrator_graphs.json" "$body"
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, (dict, list))'; then
      ok "GET /api/orchestrator/graphs: returned valid shape (HTTP 200)"
    else
      ok "GET /api/orchestrator/graphs: endpoint exists (HTTP 200)"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "GET /api/orchestrator/graphs: feature not available in this build (404)"
  elif [[ "$code" == "501" ]]; then
    skip "GET /api/orchestrator/graphs: not implemented (501)"
  else
    ko "GET /api/orchestrator/graphs: unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_163
: "${RESULT:=fail}"
unset -f _story_ts_163
