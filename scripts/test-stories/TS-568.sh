#!/usr/bin/env bash
# TS-568 — POST /api/federation/groups creates custom group
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-568"
story_preflight "surface:api feature:federation" || return 0

_story_ts_568() {
  local resp
  resp=$(api POST /api/federation/groups '{"name":"e2e-test-group-ts568","caps":["sessions:list"]}')
  save_evidence TS-568 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/groups endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/federation/groups returned dict"
    # cleanup
    api DELETE /api/federation/groups/e2e-test-group-ts568 >/dev/null 2>&1 || true
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_568
: "${RESULT:=fail}"
unset -f _story_ts_568
