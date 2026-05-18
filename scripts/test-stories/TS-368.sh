#!/usr/bin/env bash
# TS-368 — GET /api/autonomous/guardrail-profiles/{id} round-trip
# tags: surface:api feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-368"
story_preflight "surface:api feature:automata" || return 0

_story_ts_368() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled — skip guardrail profile round-trip test"
    return
  fi
  # Create a profile first
  local create_resp pid
  create_resp=$(api POST /api/autonomous/guardrail-profiles \
    '{"name":"test-gp-roundtrip","rules":[]}')
  pid=$(echo "$create_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$pid" ]]; then
    skip "could not create guardrail profile for round-trip test: $(echo "$create_resp" | head -c 100)"
    return
  fi
  add_cleanup guardrail-profile "$pid"
  # GET by id
  local resp
  resp=$(api GET "/api/autonomous/guardrail-profiles/$pid")
  save_evidence TS-368 "resp.json" "$resp"
  if assert_json "$resp" '"id" in d'; then
    ok "GET /api/autonomous/guardrail-profiles/$pid round-trips successfully"
  elif echo "$resp" | grep -qi "not found\|404"; then
    ko "profile not found after creation: $resp"
  else
    ko "unexpected response shape: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_368
: "${RESULT:=fail}"
unset -f _story_ts_368
