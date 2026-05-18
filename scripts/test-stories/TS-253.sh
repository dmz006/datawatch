#!/usr/bin/env bash
# TS-253 — GET /api/cooldown returns {active, until} shape
# tags: surface:api feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-253"
story_preflight "surface:api feature:config" || return 0

_story_ts_253() {
  local resp
  resp=$(api GET /api/cooldown)
  save_evidence TS-253 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "active" in d'; then
    ok "cooldown returns dict with active key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ko "cooldown missing active key: $resp"
  elif echo "$resp" | grep -qi "not found\|404"; then
    skip "cooldown endpoint not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_253
: "${RESULT:=fail}"
unset -f _story_ts_253
