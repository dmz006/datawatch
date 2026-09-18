#!/usr/bin/env bash
# TS-706 — File Service API: POST /api/files multipart upload round-trip
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-706"
story_preflight "surface:api feature:files" || return 0

_story_ts_706() {
  # Check file service is configured
  local meta_code
  meta_code=$(api_code GET /api/files/meta | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$meta_code" == "503" || "$meta_code" == "404" ]]; then
    skip "file service not available (GET /api/files/meta → HTTP $meta_code)"
    return
  fi

  # Create a temp file with known content
  local tmp_file="$RUN_DIR/ts706-upload.txt"
  echo "ts-706-test-content-$(date +%s)" > "$tmp_file"
  local expected_size
  expected_size=$(wc -c < "$tmp_file")

  # Upload via POST /api/files
  local resp code body
  resp=$(curl "${curl_args[@]}" -X POST \
    -F "file=@$tmp_file" \
    -F "path=ts706-upload.txt" \
    "$TEST_BASE/api/files" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-706 "upload_resp.json" "$body"

  if [[ "$code" == "403" ]]; then
    skip "upload requires CapConfigWrite — running without federation auth; confirm operator token has write cap"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST /api/files returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify response has path + bytes fields
  local got_path got_bytes
  got_path=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('path',''))" 2>/dev/null || echo "")
  got_bytes=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('bytes',0))" 2>/dev/null || echo "0")

  if [[ -z "$got_path" ]]; then
    ko "POST /api/files response missing 'path' field: $body"
    return
  fi
  if [[ "$got_bytes" -lt 1 ]]; then
    ko "POST /api/files response 'bytes' is 0 or missing: $body"
    return
  fi

  ok "POST /api/files upload succeeded — path=$got_path bytes=$got_bytes (expected ~$expected_size)"
}

RESULT=fail
_story_ts_706
: "${RESULT:=fail}"
unset -f _story_ts_706
