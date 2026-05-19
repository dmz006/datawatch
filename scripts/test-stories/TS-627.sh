#!/usr/bin/env bash
# TS-627 — GET /api/alert-rules returns 200 and rules array
# tags: surface:rest feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-627"
story_preflight "surface:rest feature:alert-rules" || return 0

_story_ts_627() {
  local resp code
  resp=$(api_code GET /api/alert-rules)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-627 "alert_rules.json" "$resp"

  if [[ "$code" == "503" || "$code" == "501" ]]; then
    skip "alert-rules endpoint not available (HTTP $code)"
    return
  fi
  if [[ "$code" != "200" ]]; then
    ko "GET /api/alert-rules expected 200, got $code"
    return
  fi
  if assert_json "$resp" 'isinstance(d.get("rules"), list)'; then
    ok "GET /api/alert-rules returns 200 and rules array"
  else
    ko "response missing rules array: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_627
: "${RESULT:=fail}"
unset -f _story_ts_627
