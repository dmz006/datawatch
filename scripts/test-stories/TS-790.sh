#!/usr/bin/env bash
# TS-790 — BL406 Phase 3: PWA session quick-command Guardrails dropdown
# includes 'project-rules-scan' alongside the 3 pre-existing built-ins.
# Static check (same shape as TS-704) — no daemon round-trip needed, the
# claim being pinned is purely "does this string exist in app.js".
# tags: surface:pwa feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-790"
story_preflight "surface:pwa feature:automata parallel:ok" || return 0

_story_ts_790() {
  local app_js="$REPO_ROOT/internal/server/web/app.js"
  if [[ ! -f "$app_js" ]]; then
    skip "app.js not found"
    return
  fi

  if grep -q "'sast-scan', 'secrets-scan', 'deps-scan', 'project-rules-scan'" "$app_js"; then
    ok "BL406 Phase 3: project-rules-scan present in the PWA Guardrails dropdown list (v9.0.14)"
  else
    ko "project-rules-scan missing from the Guardrails dropdown's built-in list in app.js"
  fi
}

RESULT=fail
_story_ts_790
: "${RESULT:=fail}"
unset -f _story_ts_790
