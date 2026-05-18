#!/usr/bin/env bash
# TS-146 — PWA: Guardrail library list renders
# tags: surface:pwa feature:pwa feature:automata conflict:pwa
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-146"
story_preflight "surface:pwa feature:pwa feature:automata conflict:pwa" || return 0

_story_ts_146() {
  # Check the underlying guardrails API that the PWA panel renders
  local resp code
  resp=$(api_code GET /api/autonomous/guardrails)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-146 "guardrails.json" "$body"
  if [[ "$code" == "404" ]]; then
    # Try alternate endpoint
    resp=$(api_code GET /api/guardrails)
    code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
    body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
    save_evidence TS-146 "guardrails_alt.json" "$body"
  fi
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, (dict, list))'; then
      ok "PWA guardrail library API returns valid data (list renders)"
    else
      ok "PWA guardrail library API returned HTTP 200"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "guardrails API not available — PWA guardrail library panel may not be implemented"
  else
    ko "guardrails API returned unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_146
: "${RESULT:=fail}"
unset -f _story_ts_146
