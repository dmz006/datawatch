#!/usr/bin/env bash
# TS-694 — memory-scope PWA tile visible on the dashboard when memory is enabled
# tags: surface:pwa feature:memory group:memory-lifecycle-v9 conflict:pwa
# pwa-script: pwa/TS-694.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-694"
story_preflight "surface:pwa feature:memory group:memory-lifecycle-v9 conflict:pwa" || return 0

_story_ts_694() {
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled — PWA tile not expected"; return; }

  if [[ ! -f "$REPO_ROOT/scripts/test-stories/pwa/TS-694.mjs" ]]; then
    skip "PWA script pwa/TS-694.mjs not yet written; mark pending"
    return
  fi

  # Driver defers to pwa-script via the standard run-tests.sh pwa harness.
  ok "PWA script present — driver will execute pwa/TS-694.mjs"
}

RESULT=fail
_story_ts_694
: "${RESULT:=fail}"
unset -f _story_ts_694
