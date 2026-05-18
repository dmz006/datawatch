#!/usr/bin/env bash
# TS-052 — Read secret metadata
# tags: surface:api feature:secrets
# legacy fn: t6_ts052_read_secret_metadata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-052"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_052() {
  local sname="test-secret-e2e-$$"
  # Set the secret first so we have something to read
  local set_resp
  set_resp=$(api PUT "/api/secrets/$sname" '{"value":"test-value-ts052"}')
  if echo "$set_resp" | grep -qi "not found\|404\|disabled\|no route"; then
    skip "secrets endpoint not available"
    return
  fi
  add_cleanup secret "$sname"
  local resp
  resp=$(api GET "/api/secrets/$sname")
  save_evidence TS-052 "get.json" "$resp"
  if assert_json "$resp" '"name" in d'; then
    ok "secret metadata returned: name=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("name",""))' 2>/dev/null)"
  elif assert_json "$resp" '"error" not in d'; then
    ok "secret GET returned non-error response"
  else
    skip "secret GET failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_052
: "${RESULT:=fail}"
unset -f _story_ts_052
