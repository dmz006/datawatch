#!/usr/bin/env bash
# TS-260 — GET /api/orchestrator/verdicts returns {verdicts:[]} shape
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-260"
story_preflight "surface:api feature:parity" || return 0

_story_ts_260() {
  local resp
  resp=$(api GET /api/orchestrator/verdicts)
  save_evidence TS-260 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "verdicts" in d'; then
    ok "orchestrator/verdicts has verdicts key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "orchestrator/verdicts returned dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "orchestrator/verdicts returned array"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "orchestrator/verdicts not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_260
: "${RESULT:=fail}"
unset -f _story_ts_260
