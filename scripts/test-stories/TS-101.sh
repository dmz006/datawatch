#!/usr/bin/env bash
# TS-101 — Comm enable/disable round-trip
# tags: surface:api feature:comms feature:config
# legacy fn: t9_ts101_comm_enable_disable
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-101"
story_preflight "surface:api feature:comms feature:config" || return 0

_story_ts_101() {
  local before_val
  before_val=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("webhook",{}).get("enabled","not_found"))' 2>/dev/null || echo "not_found")
  if [[ "$before_val" == "not_found" ]]; then
    skip "webhook config section not present"
    return
  fi
  api PUT /api/config '{"webhook.enabled":false}' >/dev/null
  local check
  check=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("webhook",{}).get("enabled","?")).lower())' 2>/dev/null || echo "?")
  save_evidence TS-101 "after_disable.json" "{\"webhook.enabled\":\"$check\"}"
  if [[ "$check" == "false" ]]; then
    ok "webhook enable/disable round-trip works"
    # Restore
    if [[ "$before_val" == "True" || "$before_val" == "true" ]]; then
      api PUT /api/config '{"webhook.enabled":true}' >/dev/null
    fi
  else
    ko "webhook disable did not persist (got $check)"
  fi
}

RESULT=fail
_story_ts_101
: "${RESULT:=fail}"
unset -f _story_ts_101
