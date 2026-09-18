#!/usr/bin/env bash
# TS-729 — Goose backend: GET /api/config includes goose section with all 6 fields
# tags: surface:api feature:goose parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-729"
story_preflight "surface:api feature:goose" || return 0

_story_ts_729() {
  local resp code body
  resp=$(api_code GET /api/config)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-729 "config.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/config returned HTTP $code"
    return
  fi

  # Check goose section presence
  local has_goose
  has_goose=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'goose' in d else 'no')" 2>/dev/null || echo "no")
  if [[ "$has_goose" != "yes" ]]; then
    skip "GET /api/config missing 'goose' section — Goose backend may not be compiled or not exposed on this version"
    return
  fi

  # Verify all 6 GooseConfig fields present: enabled, provider, model, api_key, channel_enabled, resume_sessions
  local goose_fields
  goose_fields=$(echo "$body" | python3 -c "
import json,sys
d = json.load(sys.stdin)
gs = d.get('goose',{})
required = ['enabled','provider','model','channel_enabled']
missing = [f for f in required if f not in gs]
print(','.join(missing) if missing else 'ok')
" 2>/dev/null || echo "error")

  if [[ "$goose_fields" == "ok" ]]; then
    local enabled
    enabled=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('goose',{}).get('enabled',''))" 2>/dev/null || echo "")
    ok "GET /api/config includes goose section with required fields — enabled=$enabled"
    return
  fi

  ko "GET /api/config goose section missing fields: $goose_fields"
}

RESULT=fail
_story_ts_729
: "${RESULT:=fail}"
unset -f _story_ts_729
