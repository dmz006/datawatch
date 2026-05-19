#!/usr/bin/env bash
# TS-369 — PUT /api/autonomous/guardrail_profiles/{id} updates profile
# tags: surface:api feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-369"
story_preflight "surface:api feature:automata" || return 0

_story_ts_369() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled — skip guardrail profile update test"
    return
  fi
  # Create a profile
  local create_resp pid
  create_resp=$(api POST /api/autonomous/guardrail_profiles \
    '{"name":"test-gp-update","rules":[]}')
  pid=$(echo "$create_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$pid" ]]; then
    skip "could not create guardrail profile for update test: $(echo "$create_resp" | head -c 100)"
    return
  fi
  add_cleanup guardrail-profile "$pid"
  # PUT update
  local resp code body
  resp=$(api_code PUT "/api/autonomous/guardrail_profiles/$pid" \
    '{"name":"test-gp-update-renamed","rules":[]}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-369 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    ok "PUT /api/autonomous/guardrail_profiles/$pid returned $code"
  elif [[ "$code" == "404" ]]; then
    ko "profile $pid not found for update"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_369
: "${RESULT:=fail}"
unset -f _story_ts_369
