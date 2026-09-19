#!/usr/bin/env bash
# TS-732 — Goose backend: GET /api/config round-trip for goose.enabled via PUT
# tags: surface:api feature:goose
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-732"
story_preflight "surface:api feature:goose" || return 0

_story_ts_732() {
  # First verify goose section exists
  local cfg_resp cfg
  cfg_resp=$(api_code GET /api/config)
  cfg=$(echo "$cfg_resp" | sed 's/__HTTP_CODE_[0-9]*__//')

  local has_goose
  has_goose=$(echo "$cfg" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'goose' in d else 'no')" 2>/dev/null || echo "no")
  if [[ "$has_goose" != "yes" ]]; then
    skip "GET /api/config missing 'goose' section — Goose feature not available"
    return
  fi

  # Read current enabled state
  local orig_enabled
  orig_enabled=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('goose',{}).get('enabled',False))" 2>/dev/null || echo "False")
  # Toggle: if currently false, set true and back; if true, set false and back
  local new_val
  if [[ "$orig_enabled" == "True" || "$orig_enabled" == "true" ]]; then
    new_val=false
  else
    new_val=true
  fi

  # PUT the toggle
  local put_code
  put_code=$(api_code PUT /api/config "{\"goose.enabled\":$new_val}" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$put_code" =~ ^2 ]]; then
    skip "PUT /api/config goose.enabled returned $put_code — config write not allowed"
    return
  fi

  # GET and verify the change
  local cfg2
  cfg2=$(api GET /api/config 2>/dev/null || echo "{}")
  local got_enabled
  got_enabled=$(echo "$cfg2" | python3 -c "import json,sys; print(json.load(sys.stdin).get('goose',{}).get('enabled',''))" 2>/dev/null || echo "")

  # Restore original value (Python prints True/False uppercase; JSON needs lowercase).
  local restore_val
  if [[ "$orig_enabled" == "True" || "$orig_enabled" == "true" ]]; then restore_val=true; else restore_val=false; fi
  api PUT /api/config "{\"goose.enabled\":$restore_val}" >/dev/null 2>&1 || true

  local expected
  if [[ "$new_val" == "true" ]]; then expected="True"; else expected="False"; fi
  if [[ "$got_enabled" == "$expected" || "$got_enabled" == "$new_val" ]]; then
    ok "Goose config round-trip: PUT goose.enabled=$new_val reflected in GET /api/config"
    return
  fi

  ko "Goose config PUT/GET mismatch: PUT goose.enabled=$new_val but GET returned goose.enabled=$got_enabled"
}

RESULT=fail
_story_ts_732
: "${RESULT:=fail}"
unset -f _story_ts_732
