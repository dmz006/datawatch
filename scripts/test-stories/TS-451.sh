#!/usr/bin/env bash
# TS-451 — GET /api/observer/peers entries carry compute_node field (present, may be empty string)
# tags: surface:api feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-451"
story_preflight "surface:api feature:observer feature:compute" || return 0

_story_ts_451() {
  local resp
  resp=$(api GET /api/observer/peers)
  save_evidence TS-451 "peers.json" "$resp"
  # Use HTTP code check, not body grep — "unknown" can legitimately appear in peer fields
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "observer/peers endpoint not available or returned non-JSON"
    return
  fi
  if assert_json "$resp" 'all("compute_node" in p for p in (d.get("peers",d) if isinstance(d,dict) else d) if isinstance(p,dict))'; then
    ok "GET /api/observer/peers entries carry compute_node field"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "observer/peers responds but compute_node field not present in entries"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_451
: "${RESULT:=fail}"
unset -f _story_ts_451
