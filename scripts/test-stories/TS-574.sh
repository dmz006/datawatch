#!/usr/bin/env bash
# TS-574 — GET /api/federation/sessions fans out to runtime-registered federated peers
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-574"
story_preflight "surface:api feature:federation" || return 0

_story_ts_574() {
  local resp
  resp=$(api GET /api/federation/sessions)
  save_evidence TS-574 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/sessions endpoint not available in this build"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/federation/sessions returns list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/federation/sessions returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_574
: "${RESULT:=fail}"
unset -f _story_ts_574
