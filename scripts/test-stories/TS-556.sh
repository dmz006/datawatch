#!/usr/bin/env bash
# TS-556 — All test stories present (meta-test)
# Counts every TS-NNN story across all surfaces and verifies the suite
# has not lost stories below a historical minimum. Update MIN_STORIES when
# adding a batch of new tests (keep ~20 below the expected total so minor
# deliberate deletions don't fail the gate).
# tags: surface:meta
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-556"
story_preflight "surface:meta" || return 0

MIN_STORIES=550

_story_ts_556() {
  local story_dir
  story_dir="$(dirname "${BASH_SOURCE[0]}")"

  local sh_count pwa_count total
  sh_count=$(find "$story_dir" -maxdepth 1 -name "TS-[0-9]*.sh" | wc -l)
  pwa_count=$(find "$story_dir/pwa" -maxdepth 1 -name "TS-[0-9]*.mjs" 2>/dev/null | wc -l)
  total=$(( sh_count + pwa_count ))

  save_evidence TS-556 "story_count.txt" "total=$total  shell=$sh_count  pwa=$pwa_count  min=$MIN_STORIES"

  if [[ $total -ge $MIN_STORIES ]]; then
    ok "meta: $total test stories present ($sh_count shell + $pwa_count pwa; min=$MIN_STORIES)"
  else
    ko "meta: only $total test stories found — expected >=$MIN_STORIES (stories may have been deleted)"
  fi
}

RESULT=fail
_story_ts_556
: "${RESULT:=fail}"
unset -f _story_ts_556
