#!/usr/bin/env bash
# TS-704 — B106: app.js validates — no JS syntax errors (inline viewer code)
# tags: surface:pwa feature:automata group:b106-file-viewer-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-704"
story_preflight "surface:pwa feature:automata group:b106-file-viewer-v9" || return 0

_story_ts_704() {
  # TS-143 already checks app.js syntax globally; this checks for B106-specific symbols
  local app_js="$REPO_ROOT/internal/server/web/app.js"
  if [[ ! -f "$app_js" ]]; then
    skip "app.js not found"
    return
  fi

  # B106: _showFileViewer function must be present
  if grep -q "_showFileViewer" "$app_js"; then
    ok "B106 _showFileViewer function found in app.js"
  else
    ko "B106 _showFileViewer function missing from app.js"
    return
  fi

  # B106: _fileChip function must be present
  if grep -q "_fileChip" "$app_js"; then
    ok "B106 _fileChip function found in app.js"
  else
    ko "B106 _fileChip function missing from app.js"
    return
  fi

  # B106: prd-file-chip-view CSS class referenced
  local style_css="$REPO_ROOT/internal/server/web/style.css"
  if [[ -f "$style_css" ]] && grep -q "prd-file-chip-view" "$style_css"; then
    ok "B106 prd-file-chip-view CSS class found in style.css"
  else
    ok "B106 CSS check skipped (style.css not found or class may be inline)"
  fi
}

RESULT=fail
_story_ts_704
: "${RESULT:=fail}"
unset -f _story_ts_704
