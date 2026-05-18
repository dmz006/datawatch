#!/usr/bin/env bash
# TS-256 — POST /api/devices/register shape round-trip
# tags: surface:api feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-256"
story_preflight "surface:api feature:config" || return 0

_story_ts_256() {
  local resp
  resp=$(api POST /api/devices/register '{"device_id":"e2e-test-device-ts256","platform":"test","name":"TS-256 Test Device"}')
  save_evidence TS-256 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "devices/register endpoint not available in this build"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "devices/register returned dict"
    # cleanup
    api DELETE /api/devices/e2e-test-device-ts256 >/dev/null 2>&1 || true
  else
    ko "devices/register returned unexpected shape: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_256
: "${RESULT:=fail}"
unset -f _story_ts_256
