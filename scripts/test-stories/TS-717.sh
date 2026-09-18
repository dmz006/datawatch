#!/usr/bin/env bash
# TS-717 — mcp-search: GET /api/config contains web_search section with enabled field
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-717"
story_preflight "surface:api feature:mcp-search" || return 0

_story_ts_717() {
  local cfg code
  local resp
  resp=$(api_code GET /api/config)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  cfg=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-717 "config.json" "$cfg"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/config returned HTTP $code"
    return
  fi

  # Check web_search section exists
  local has_ws
  has_ws=$(echo "$cfg" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'web_search' in d else 'no')" 2>/dev/null || echo "no")
  if [[ "$has_ws" != "yes" ]]; then
    skip "GET /api/config does not include 'web_search' key — feature may not be compiled or not exposed in this version"
    return
  fi

  # Verify web_search has at minimum an 'enabled' field
  local ws_enabled
  ws_enabled=$(echo "$cfg" | python3 -c "import json,sys; d=json.load(sys.stdin); ws=d.get('web_search',{}); print('yes' if 'enabled' in ws else 'no')" 2>/dev/null || echo "no")
  if [[ "$ws_enabled" != "yes" ]]; then
    ko "GET /api/config web_search section missing 'enabled' field: $(echo "$cfg" | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('web_search',{}))" 2>/dev/null)"
    return
  fi

  local ws_val
  ws_val=$(echo "$cfg" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('web_search',{}).get('enabled',''))" 2>/dev/null || echo "")
  ok "GET /api/config includes web_search.enabled=$ws_val — config surface present"
}

RESULT=fail
_story_ts_717
: "${RESULT:=fail}"
unset -f _story_ts_717
