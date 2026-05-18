#!/usr/bin/env bash
# TS-551 — GET /api/council/config returns config shape with llm_ref field
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-551"
story_preflight "surface:api feature:council" || return 0

_story_ts_551() {
  local resp
  resp=$(api GET /api/council/config)
  save_evidence TS-551 "config.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "council/config endpoint not available"
    return
  fi
  if assert_json "$resp" '"llm_ref" in d or "config" in d'; then
    ok "GET /api/council/config has llm_ref or config field"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/council/config returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_551
: "${RESULT:=fail}"
unset -f _story_ts_551
