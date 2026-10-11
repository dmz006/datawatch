#!/usr/bin/env bash
# TS-791 — v9.0.3: About page's orphaned-tmux card must be federation-
# aware (apiFetch, not a bare fetch to the local origin). Static check:
# the real bug was a bare fetch('/api/stats') inside loadAboutOrphanedTmux
# always hitting the local daemon regardless of which federated server
# was selected — pin the fixed call site directly.
# tags: surface:pwa feature:federation parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-791"
story_preflight "surface:pwa feature:federation parallel:ok" || return 0

_story_ts_791() {
  local app_js="$REPO_ROOT/internal/server/web/app.js"
  if [[ ! -f "$app_js" ]]; then
    skip "app.js not found"
    return
  fi

  # Extract the function body between its own def and the next top-level
  # function/window-assignment line, then check it calls apiFetch and
  # never a bare fetch() to '/api/stats'.
  local body
  body=$(awk '/^function loadAboutOrphanedTmux\(\)/{p=1} p{print} p && /^window\.loadAboutOrphanedTmux/{exit}' "$app_js")

  if [[ -z "$body" ]]; then
    ko "loadAboutOrphanedTmux function not found in app.js"
    return
  fi
  if echo "$body" | grep -q "apiFetch('/api/stats')"; then
    if echo "$body" | grep -qE "[^.]fetch\('/api/stats'\)"; then
      ko "loadAboutOrphanedTmux calls apiFetch but ALSO still has a bare fetch('/api/stats') call"
    else
      ok "loadAboutOrphanedTmux uses the federation-aware apiFetch(), not a bare fetch() (v9.0.3 regression pin)"
    fi
  else
    ko "loadAboutOrphanedTmux does not call apiFetch('/api/stats') — federation-awareness regressed"
  fi
}

RESULT=fail
_story_ts_791
: "${RESULT:=fail}"
unset -f _story_ts_791
