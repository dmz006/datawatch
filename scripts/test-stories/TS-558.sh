#!/usr/bin/env bash
# TS-558 — docs/testing/master-cookbook.md has no planned stories
# tags: surface:meta
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-558"
story_preflight "surface:meta" || return 0

_story_ts_558() {
  local cookbook="$REPO_ROOT/docs/testing/master-cookbook.md"
  if [[ ! -f "$cookbook" ]]; then
    skip "master-cookbook.md not found"
    return
  fi
  local count
  count=$(grep -c "📋 planned" "$cookbook" 2>/dev/null || echo "0")
  if [[ "$count" -eq 0 ]]; then
    ok "master-cookbook.md has no planned stories"
  else
    ko "master-cookbook.md has $count planned stories remaining"
  fi
}

RESULT=fail
_story_ts_558
: "${RESULT:=fail}"
unset -f _story_ts_558
