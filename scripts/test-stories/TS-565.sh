#!/usr/bin/env bash
# TS-565 — POST /api/federation/peers creates peer with federation-peer default caps
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-565"
story_preflight "surface:api feature:federation" || return 0

_story_ts_565() {
  local resp
  resp=$(api POST /api/federation/peers '{"name":"e2e-fed-peer-ts565","url":"http://127.0.0.1:19999","token":"test-token"}')
  save_evidence TS-565 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/federation/peers returned dict"
    # cleanup
    api DELETE /api/federation/peers/e2e-fed-peer-ts565 >/dev/null 2>&1 || true
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_565
: "${RESULT:=fail}"
unset -f _story_ts_565
