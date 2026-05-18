#!/usr/bin/env bash
# TS-563 — scripts/release-smoke.sh §42 howto-existence guard: mcp-sampling.md and mcp-elicitation.md exist
# tags: surface:build
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-563"
story_preflight "surface:build" || return 0

_story_ts_563() {
  local missing=0
  for doc in "mcp-sampling.md" "mcp-elicitation.md"; do
    local found=0
    for dir in "$REPO_ROOT/docs" "$REPO_ROOT/docs/howto"; do
      if [[ -f "$dir/$doc" ]]; then
        found=1
        break
      fi
    done
    if [[ $found -eq 0 ]]; then
      ko "$doc not found in docs/ or docs/howto/"
      missing=1
    fi
  done
  [[ $missing -eq 0 ]] && ok "mcp-sampling.md and mcp-elicitation.md found in docs tree"
}

RESULT=fail
_story_ts_563
: "${RESULT:=fail}"
unset -f _story_ts_563
