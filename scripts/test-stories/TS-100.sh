#!/usr/bin/env bash
# TS-100 — comm_stats shape after all sends
# tags: surface:api feature:comms
# legacy fn: t9_ts100_comm_stats_shape
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-100"
story_preflight "surface:api feature:comms" || return 0

_story_ts_100() {
  local resp
  resp=$(api GET /api/stats)
  save_evidence TS-100 "comm_stats.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "stats returns dict (comm_stats extractable)"
  else
    ko "stats shape wrong: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_100
: "${RESULT:=fail}"
unset -f _story_ts_100
