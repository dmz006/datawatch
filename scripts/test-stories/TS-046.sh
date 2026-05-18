#!/usr/bin/env bash
# TS-046 — KG stats
# tags: surface:api feature:kg
# legacy fn: t5_ts046_kg_stats
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-046"
story_preflight "surface:api feature:kg" || return 0

_story_ts_046() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(api GET /api/memory/kg/stats)
  save_evidence TS-046 "stats.json" "$resp"
  if assert_json "$resp" 'all(k in d for k in ("entity_count","triple_count","active_count","expired_count"))'; then
    ok "KG stats has all 4 counters"
  else
    ko "KG stats missing counters: $resp"
  fi
}

RESULT=fail
_story_ts_046
: "${RESULT:=fail}"
unset -f _story_ts_046
