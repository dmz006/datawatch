#!/usr/bin/env bash
# TS-623 — v8.0 smoke — PWA root returns HTML with title tag
# tags: surface:pwa feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-623"
story_preflight "surface:pwa feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_623() {
  local code body
  code=$(curl "${curl_args[@]}" -o /tmp/r623_root.html -w "%{http_code}" "$TEST_BASE/" 2>/dev/null || echo "000")
  body=$(cat /tmp/r623_root.html 2>/dev/null || echo "")
  save_evidence TS-623 "root.html" "$body"

  if [[ "$code" != "200" ]]; then
    ko "GET / expected 200, got $code"
    return
  fi

  if echo "$body" | grep -qi "<title>"; then
    ok "PWA root returns HTTP 200 with <title> tag"
  else
    ko "PWA root response missing <title> tag: $(echo "$body" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_623
: "${RESULT:=fail}"
unset -f _story_ts_623
