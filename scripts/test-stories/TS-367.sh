#!/usr/bin/env bash
# TS-367 — POST /api/autonomous/guardrail_profiles creates profile
# tags: surface:api feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-367"
story_preflight "surface:api feature:automata" || return 0

_story_ts_367() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled — skip guardrail profile test"
    return
  fi
  local resp code body
  resp=$(api_code POST /api/autonomous/guardrail_profiles \
    '{"name":"test-guardrail-profile","rules":[]}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-367 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "201" ]]; then
    local pid
    pid=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
    if [[ -n "$pid" ]]; then
      add_cleanup guardrail-profile "$pid"
      ok "POST /api/autonomous/guardrail_profiles returned $code with id=$pid"
    else
      ok "POST /api/autonomous/guardrail_profiles returned $code"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "guardrail-profiles endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_367
: "${RESULT:=fail}"
unset -f _story_ts_367
