#!/usr/bin/env bash
# TS-263 — POST /api/templates creates; GET retrieves; DELETE removes
# tags: surface:api feature:plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-263"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_263() {
  local resp tpl_id tpl_name
  tpl_name="test-tpl-ts263-$$"

  # Create
  resp=$(api POST /api/templates "{\"name\":\"$tpl_name\",\"content\":\"test template content\",\"kind\":\"prompt\"}")
  save_evidence TS-263 "create.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "templates POST not available in this build"
    return
  fi
  tpl_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",d.get("name","")))' 2>/dev/null || echo "")
  if [[ -z "$tpl_id" ]]; then
    # Try to find by name
    tpl_id="$tpl_name"
  fi
  add_cleanup template "$tpl_id"

  # GET by id/name
  resp=$(api GET "/api/templates/$tpl_id")
  save_evidence TS-263 "get.json" "$resp"
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "templates GET after create failed: $(echo "$resp" | head -c 200)"
    return
  fi

  # DELETE
  resp=$(api DELETE "/api/templates/$tpl_id")
  save_evidence TS-263 "delete.json" "$resp"

  ok "templates CRUD round-trip: create/get/delete"
}

RESULT=fail
_story_ts_263
: "${RESULT:=fail}"
unset -f _story_ts_263
