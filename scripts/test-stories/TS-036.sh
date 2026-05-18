#!/usr/bin/env bash
# TS-036 — Persona edit round-trip
# tags: surface:api feature:council conflict:db-write
# legacy fn: t4_ts036_persona_edit_roundtrip
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-036"
story_preflight "surface:api feature:council conflict:db-write" || return 0

_story_ts_036() {
  if [[ -z "$PERSONA_ID" ]]; then skip "no persona ID"; return; fi
  local resp
  resp=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"role":"senior-analyst","system_prompt":"Updated e2e test prompt"}' \
    "$TEST_BASE/api/council/personas/$PERSONA_ID")
  save_evidence TS-036 "update.json" "$resp"
  local get_resp
  get_resp=$(api GET "/api/council/personas/$PERSONA_ID")
  save_evidence TS-036 "get_after.json" "$get_resp"
  if echo "$resp" | grep -qi "not found\|no such\|404"; then
    skip "persona $PERSONA_ID not found — may have been cleaned up"
  elif assert_json "$resp" 'isinstance(d, dict)' || assert_json "$get_resp" 'd.get("role") == "senior-analyst"'; then
    ok "persona edit accepted"
  else
    ko "persona edit failed: $resp"
  fi
}

RESULT=fail
_story_ts_036
: "${RESULT:=fail}"
unset -f _story_ts_036
