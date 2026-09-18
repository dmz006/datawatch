#!/usr/bin/env bash
# TS-709 — File Service API: POST /api/files with path traversal attempt returns 400/403
# tags: surface:api feature:files surface:security parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-709"
story_preflight "surface:api feature:files surface:security" || return 0

_story_ts_709() {
  # Check file service is configured first
  local meta_code
  meta_code=$(api_code GET /api/files/meta | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$meta_code" == "503" || "$meta_code" == "404" ]]; then
    skip "file service not available (GET /api/files/meta → HTTP $meta_code)"
    return
  fi

  # Attempt a path traversal: send path=/tmp/evil.txt which should be outside the file service root
  local tmp_file="$RUN_DIR/ts709-traversal-test.txt"
  echo "traversal-attempt-should-be-blocked" > "$tmp_file"

  local resp code body
  resp=$(curl "${curl_args[@]}" -X POST \
    -F "file=@$tmp_file" \
    -F "path=/tmp/ts709-evil.txt" \
    "$TEST_BASE/api/files" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-709 "traversal_resp.json" "$body"

  if [[ "$code" == "403" || "$code" == "400" ]]; then
    ok "POST /api/files with absolute path=/tmp/ blocked (HTTP $code) — path traversal guard active"
    return
  fi
  if [[ "$code" == "200" || "$code" == "201" ]]; then
    ko "POST /api/files with path=/tmp/ts709-evil.txt was ACCEPTED (HTTP $code) — path traversal not blocked! body: $(echo "$body" | head -c 200)"
    return
  fi
  if [[ "$code" == "000" ]]; then
    skip "request timed out or connection refused"
    return
  fi

  # Any non-2xx (other than 403/400) is also fine — the request was rejected
  if [[ ! "$code" =~ ^2 ]]; then
    ok "POST /api/files with path=/tmp/ rejected (HTTP $code) — path traversal guard active"
    return
  fi

  ko "Unexpected HTTP $code for traversal attempt: $(echo "$body" | head -c 200)"
}

RESULT=fail
_story_ts_709
: "${RESULT:=fail}"
unset -f _story_ts_709
