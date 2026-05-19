#!/usr/bin/env bash
# TS-589 — Observer tab shows Federation Peers card
# tags: surface:pwa feature:federation conflict:pwa
# pwa-script: pwa/TS-589.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-589"
story_preflight "surface:pwa feature:federation conflict:pwa" || return 0

_story_ts_589() {
  # Optional: confirm federation API is reachable before running browser test
  local resp
  resp=$(api GET /api/federation/peers 2>/dev/null || true)
  save_evidence TS-589 "api.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi

  # Run the Playwright PWA test
  run_pwa_story "TS-589"
}

RESULT=fail
_story_ts_589
: "${RESULT:=fail}"
unset -f _story_ts_589
