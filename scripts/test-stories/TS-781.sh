#!/usr/bin/env bash
# TS-781 — detection.alert_settle / detection.alert_repeat config round-trip (REST PUT -> GET).
# tags: surface:api feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-781"
story_preflight "surface:api feature:config" || return 0

_story_ts_781() {
  local before resp
  before=$(api GET /api/config)
  api PUT /api/config '{"detection.alert_settle": 77, "detection.alert_repeat": 222}' >/dev/null
  resp=$(api GET /api/config)
  if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin)['detection']; sys.exit(0 if d['alert_settle']==77 and d['alert_repeat']==222 else 1)"; then
    ok "alert_settle/alert_repeat round-trip via PUT then GET /api/config"
  else
    ko "alert_settle/alert_repeat did not round-trip: $(echo "$resp" | head -c 200)"
  fi
  local s r
  s=$(echo "$before" | python3 -c "import json,sys; print(json.load(sys.stdin)['detection']['alert_settle'])" 2>/dev/null || echo 45)
  r=$(echo "$before" | python3 -c "import json,sys; print(json.load(sys.stdin)['detection']['alert_repeat'])" 2>/dev/null || echo 300)
  api PUT /api/config "{\"detection.alert_settle\": $s, \"detection.alert_repeat\": $r}" >/dev/null
}

RESULT=fail
_story_ts_781
: "${RESULT:=fail}"
unset -f _story_ts_781
