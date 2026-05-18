#!/usr/bin/env bash
# TS-560 — node --check internal/server/web/app.js exits 0
# tags: surface:build
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-560"
story_preflight "surface:build" || return 0

_story_ts_560() {
  local app_js="$REPO_ROOT/internal/server/web/app.js"
  if [[ ! -f "$app_js" ]]; then
    skip "app.js not found at $app_js"
    return
  fi
  if ! command -v node >/dev/null 2>&1; then
    skip "node not in PATH"
    return
  fi
  local out rc
  out=$(node --check "$app_js" 2>&1); rc=$?
  save_evidence TS-560 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "node --check app.js exits 0"
  else
    ko "node --check app.js rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_560
: "${RESULT:=fail}"
unset -f _story_ts_560
