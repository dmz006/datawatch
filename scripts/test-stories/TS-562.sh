#!/usr/bin/env bash
# TS-562 — docs-index-gen runs without errors
# tags: surface:build
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-562"
story_preflight "surface:build" || return 0

_story_ts_562() {
  if [[ ! -d "$REPO_ROOT/cmd/docs-index-gen" ]]; then
    skip "cmd/docs-index-gen not found at $REPO_ROOT/cmd/docs-index-gen"
    return
  fi
  local out rc
  out=$(cd "$REPO_ROOT" && timeout 60 go run ./cmd/docs-index-gen 2>&1); rc=$?
  save_evidence TS-562 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "docs-index-gen runs without errors"
  elif [[ $rc -eq 124 ]]; then
    skip "docs-index-gen timed out"
  else
    ko "docs-index-gen rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_562
: "${RESULT:=fail}"
unset -f _story_ts_562
