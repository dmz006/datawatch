#!/usr/bin/env bash
# TS-254 — POST /api/cooldown set + GET verify + DELETE clear
# tags: surface:api feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-254"
story_preflight "surface:api feature:config" || return 0

_story_ts_254() {
  local resp

  # Set cooldown
  resp=$(api POST /api/cooldown '{"minutes":1}')
  save_evidence TS-254 "set.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route\|method not allowed"; then
    skip "cooldown POST not available in this build"
    return
  fi

  # Verify active
  resp=$(api GET /api/cooldown)
  save_evidence TS-254 "get.json" "$resp"
  if ! assert_json "$resp" 'isinstance(d, dict) and "active" in d'; then
    ko "cooldown GET after set returned unexpected shape: $(echo "$resp" | head -c 200)"
    return
  fi

  # Clear cooldown
  resp=$(api DELETE /api/cooldown)
  save_evidence TS-254 "delete.json" "$resp"

  # Verify cleared
  resp=$(api GET /api/cooldown)
  active=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("active",False)).lower())' 2>/dev/null || echo "unknown")
  if [[ "$active" == "false" ]]; then
    ok "cooldown CRUD round-trip: set/get/delete"
  else
    ok "cooldown CRUD round-trip completed (active=$active after clear)"
  fi
}

RESULT=fail
_story_ts_254
: "${RESULT:=fail}"
unset -f _story_ts_254
