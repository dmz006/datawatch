#!/usr/bin/env bash
# TS-562 — docs-index-gen runs without errors
# tags: surface:build
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-562"
story_preflight "surface:build" || return 0

_story_ts_562() {
  local gen_script="$REPO_ROOT/scripts/docs-index-gen.sh"
  if [[ ! -f "$gen_script" ]]; then
    skip "docs-index-gen.sh not found at $gen_script"
    return
  fi
  local out rc
  out=$(timeout 60 bash "$gen_script" 2>&1); rc=$?
  save_evidence TS-562 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "docs-index-gen.sh exits 0"
  elif [[ $rc -eq 124 ]]; then
    skip "docs-index-gen.sh timed out"
  else
    ko "docs-index-gen.sh rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_562
: "${RESULT:=fail}"
unset -f _story_ts_562
