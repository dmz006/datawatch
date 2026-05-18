#!/usr/bin/env bash
# TS-136 — Alerts panel renders
# tags: surface:pwa feature:bootstrap conflict:pwa
# legacy fn: t11_ts136_alerts_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-136"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_136() {
  local resp
  resp=$(api GET /api/alerts)
  save_evidence TS-136 "alerts.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("alerts",[]), list)'; then
    ok "alerts endpoint works"
  else
    ko "alerts endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_136
: "${RESULT:=fail}"
unset -f _story_ts_136
