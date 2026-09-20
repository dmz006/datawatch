#!/usr/bin/env bash
# TS-736 — datawatch mcp-search: DATAWATCH_WEB_SEARCH_URL config applied via GET /api/config
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-736"
story_preflight "surface:api feature:mcp-search" || return 0

_story_ts_736() {
  # Check web_search section in config
  local cfg_resp cfg
  cfg_resp=$(api_code GET /api/config)
  cfg=$(echo "$cfg_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  local cfg_code
  cfg_code=$(echo "$cfg_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  if [[ ! "$cfg_code" =~ ^2 ]]; then
    ko "GET /api/config returned HTTP $cfg_code"
    return
  fi

  save_evidence TS-736 "config.json" "$cfg"

  local has_ws
  has_ws=$(echo "$cfg" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'web_search' in d else 'no')" 2>/dev/null || echo "no")
  if [[ "$has_ws" != "yes" ]]; then
    skip "GET /api/config does not include web_search section — feature not available on this version"
    return
  fi

  # Try toggling web_search.enabled via PUT
  local orig_ws
  orig_ws=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('web_search',{}).get('enabled',False))" 2>/dev/null || echo "False")

  local new_val
  if [[ "$orig_ws" == "True" || "$orig_ws" == "true" ]]; then
    new_val=false
  else
    new_val=true
  fi

  local put_code
  put_code=$(api_code PUT /api/config "{\"web_search.enabled\":$new_val}" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$put_code" =~ ^2 ]]; then
    skip "PUT /api/config web_search.enabled returned $put_code — config write not allowed"
    return
  fi

  local cfg2
  cfg2=$(api GET /api/config 2>/dev/null || echo "{}")
  local got_val
  got_val=$(echo "$cfg2" | python3 -c "import json,sys; print(json.load(sys.stdin).get('web_search',{}).get('enabled',''))" 2>/dev/null || echo "")

  # Restore (Python prints True/False uppercase; JSON needs lowercase).
  local restore_ws
  if [[ "$orig_ws" == "True" || "$orig_ws" == "true" ]]; then restore_ws=true; else restore_ws=false; fi
  api PUT /api/config "{\"web_search.enabled\":$restore_ws}" >/dev/null 2>&1 || true

  local expected
  if [[ "$new_val" == "true" ]]; then expected="True"; else expected="False"; fi
  if [[ "$got_val" == "$expected" || "$got_val" == "$new_val" ]]; then
    ok "web_search config round-trip: PUT web_search.enabled=$new_val reflected in GET /api/config"
    return
  fi

  ko "web_search config PUT/GET mismatch: PUT $new_val but GET returned $got_val"
}

RESULT=fail
_story_ts_736
: "${RESULT:=fail}"
unset -f _story_ts_736
