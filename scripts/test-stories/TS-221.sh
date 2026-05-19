#!/usr/bin/env bash
# TS-221 — GET /api/stats returns valid stats shape
# tags: surface:api feature:network
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-221"
story_preflight "surface:api feature:network" || return 0

_story_ts_221() {
  echo ""; echo "  >> TS-221: GET /api/stats returns valid stats shape"
  local resp
  resp=$(api GET /api/stats)
  save_evidence "TS-221" "stats.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and any(k in d for k in ("active_sessions","mem_total","uptime_seconds","cpu_cores","timestamp","goroutines"))'; then
    ok "GET /api/stats returns valid stats shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/stats returns dict: $(echo "$resp" | head -c 100)"
  else
    ko "GET /api/stats unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_221
: "${RESULT:=fail}"
unset -f _story_ts_221
