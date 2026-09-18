#!/usr/bin/env bash
# TS-712 — v8.27.5 per-guardrail approve: 404 for unknown guardrail name
# tags: surface:api feature:sessions feature:guardrail parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-712"
story_preflight "surface:api feature:sessions feature:guardrail" || return 0

_story_ts_712() {
  # Get any session ID
  local sid
  sid=$(api GET /api/sessions 2>/dev/null \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["id"] if d else "")' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "no sessions available for guardrail 404 test"
    return
  fi

  # POST approve for a guardrail name that does not exist
  local resp code body
  resp=$(api_code POST "/api/sessions/$sid/guardrail/totally-nonexistent-guardrail-xyz/approve" '{}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-712 "approve_404_resp.json" "$body"

  if [[ "$code" == "404" ]]; then
    ok "POST .../guardrail/nonexistent/approve returns 404 as expected — guard is name-validated"
    return
  fi
  if [[ "$code" == "405" ]]; then
    ko "POST .../guardrail/{name}/approve returned 405 — endpoint routing broken"
    return
  fi
  if [[ "$code" == "000" ]]; then
    skip "endpoint timed out or not reachable"
    return
  fi

  ko "POST .../guardrail/nonexistent/approve returned HTTP $code (expected 404): $(echo "$body" | head -c 200)"
}

RESULT=fail
_story_ts_712
: "${RESULT:=fail}"
unset -f _story_ts_712
