#!/usr/bin/env bash
# TS-152 — Observer peers surface
# tags: surface:api feature:agents
# legacy fn: t12_ts152_observer_peers
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-152"
story_preflight "surface:api feature:agents" || return 0

_story_ts_152() {
  local resp
  resp=$(api GET /api/observer/peers)
  save_evidence TS-152 "peers.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/observer/peers returns valid shape"
  else
    ko "observer peers unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_152
: "${RESULT:=fail}"
unset -f _story_ts_152
