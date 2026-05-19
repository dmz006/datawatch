#!/usr/bin/env bash
# TS-632 — GET /api/alert-rules/firings returns 200 and firings array
# tags: surface:rest feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-632"
story_preflight "surface:rest feature:alert-rules" || return 0

_story_ts_632() {
  local resp code
  resp=$(api_code GET /api/alert-rules/firings)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-632 "firings.json" "$resp"

  if [[ "$code" == "503" || "$code" == "501" ]]; then
    skip "alert-rules/firings endpoint not available (HTTP $code)"
    return
  fi
  if [[ "$code" != "200" ]]; then
    ko "GET /api/alert-rules/firings expected 200, got $code"
    return
  fi
  if assert_json "$resp" 'isinstance(d.get("firings"), list)'; then
    ok "GET /api/alert-rules/firings returns 200 and firings array"
  else
    ko "response missing firings array: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_632
: "${RESULT:=fail}"
unset -f _story_ts_632
