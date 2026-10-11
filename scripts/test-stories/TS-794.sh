#!/usr/bin/env bash
# TS-794 — v9.0.16: notify_exclude config round-trip. Restores whatever
# was there before so other stories aren't affected by a leftover
# exclusion list.
# tags: surface:api feature:messaging conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-794"
story_preflight "surface:api feature:messaging conflict:selfconfig" || return 0

_story_ts_794() {
  local before
  before=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.dumps(d.get("notify_exclude",[])))' 2>/dev/null || echo "[]")

  api PUT /api/config '{"notify_exclude":["slack","ntfy"]}' >/dev/null
  local after
  after=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.dumps(sorted(d.get("notify_exclude",[]))))' 2>/dev/null || echo "[]")

  api PUT /api/config "{\"notify_exclude\":$before}" >/dev/null

  if [[ "$after" == '["ntfy", "slack"]' ]]; then
    ok "notify_exclude round-trips through PUT/GET /api/config (v9.0.16)"
  else
    ko "notify_exclude round-trip mismatch, got $after"
  fi
}

RESULT=fail
_story_ts_794
: "${RESULT:=fail}"
unset -f _story_ts_794
