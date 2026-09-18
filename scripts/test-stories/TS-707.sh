#!/usr/bin/env bash
# TS-707 — File Service API: DELETE /api/files round-trip (upload then delete)
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-707"
story_preflight "surface:api feature:files" || return 0

_story_ts_707() {
  # Check file service is configured
  local meta_code
  meta_code=$(api_code GET /api/files/meta | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$meta_code" == "503" || "$meta_code" == "404" ]]; then
    skip "file service not available (GET /api/files/meta → HTTP $meta_code)"
    return
  fi

  # Upload a file first
  local tmp_file="$RUN_DIR/ts707-delete-test.txt"
  echo "ts-707-to-be-deleted-$(date +%s)" > "$tmp_file"
  local name="ts707-delete-$(date +%s).txt"

  local up_resp up_code up_body
  up_resp=$(curl "${curl_args[@]}" -X POST \
    -F "file=@$tmp_file" \
    -F "path=$name" \
    "$TEST_BASE/api/files" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  up_code=$(echo "$up_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  up_body=$(echo "$up_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-707 "upload_resp.json" "$up_body"

  if [[ "$up_code" == "403" ]]; then
    skip "upload requires CapConfigWrite — running without federation auth"
    return
  fi
  if [[ ! "$up_code" =~ ^2 ]]; then
    ko "POST /api/files returned HTTP $up_code — cannot proceed with delete test"
    return
  fi

  local uploaded_path
  uploaded_path=$(echo "$up_body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('path',''))" 2>/dev/null || echo "")
  if [[ -z "$uploaded_path" ]]; then
    ko "upload returned no path — cannot delete"
    return
  fi

  # Now delete via DELETE /api/files?path=<path>
  local del_resp del_code del_body
  del_resp=$(curl "${curl_args[@]}" -X DELETE \
    "$TEST_BASE/api/files?path=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$uploaded_path'))")" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  del_code=$(echo "$del_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  del_body=$(echo "$del_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-707 "delete_resp.json" "$del_body"

  if [[ ! "$del_code" =~ ^2 ]]; then
    ko "DELETE /api/files returned HTTP $del_code: $(echo "$del_body" | head -c 200)"
    return
  fi

  ok "DELETE /api/files round-trip succeeded — uploaded $uploaded_path then deleted (HTTP $del_code)"
}

RESULT=fail
_story_ts_707
: "${RESULT:=fail}"
unset -f _story_ts_707
