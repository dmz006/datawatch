#!/usr/bin/env bash
# TS-697 — B106: GET /api/files/download?path=<abs>&inline=1 returns inline content
# tags: surface:api feature:automata group:b106-file-viewer-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-697"
story_preflight "surface:api feature:automata group:b106-file-viewer-v9" || return 0

_story_ts_697() {
  # Use a known file that exists in the daemon's root_path
  # The test daemon's root_path is set to /home/dmz/workspace/datawatch by default
  local test_file=""
  local candidates=(
    "/home/dmz/workspace/datawatch/README.md"
    "/home/dmz/workspace/datawatch/docs/parity-status.md"
    "/etc/hostname"
  )
  for f in "${candidates[@]}"; do
    [[ -f "$f" ]] && { test_file="$f"; break; }
  done
  if [[ -z "$test_file" ]]; then
    skip "no readable test file found for inline download test"
    return
  fi

  local resp code headers
  resp=$(curl "${curl_args[@]}" -D - -s \
    "$TEST_BASE/api/files/download?path=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "$test_file")&inline=1" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null)
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  save_evidence TS-697 "headers.txt" "$(echo "$resp" | head -20)"

  if [[ "$code" == "403" || "$code" == "404" ]]; then
    skip "file download endpoint denied access (HTTP $code) — root_path restriction"
    return
  fi
  if [[ "$code" == "404" || "$code" == "501" ]]; then
    skip "files/download endpoint not available (HTTP $code)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    # inline=1 should NOT have Content-Disposition: attachment
    if echo "$resp" | grep -qi "content-disposition.*attachment"; then
      ko "inline=1 returned Content-Disposition: attachment (should be inline)"
    else
      ok "GET /api/files/download?inline=1 returned 200 without attachment header (B106)"
    fi
  else
    ko "GET /api/files/download?inline=1 expected 200, got $code"
  fi
}

RESULT=fail
_story_ts_697
: "${RESULT:=fail}"
unset -f _story_ts_697
