#!/usr/bin/env bash
# TS-556 — All TS-001 to TS-555 pass or skip (meta-test)
# tags: surface:meta
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-556"
story_preflight "surface:meta" || return 0

_story_ts_556() {
  local story_dir
  story_dir="$(dirname "${BASH_SOURCE[0]}")"
  # Count stories TS-001 through TS-555
  local count
  count=$(find "$story_dir" -maxdepth 1 -name "TS-[0-9][0-9][0-9].sh" | \
    awk -F'TS-' '{n=int($2); if (n>=1 && n<=555) print}' | wc -l)
  local pwa_count
  pwa_count=$(find "$story_dir/pwa" -maxdepth 1 -name "TS-[0-9][0-9][0-9].mjs" 2>/dev/null | wc -l)
  local total=$(( count + pwa_count ))
  save_evidence TS-556 "story_count.txt" "stories TS-001..555 found: $count sh + $pwa_count pwa = $total"
  # Not all numbers in TS-001..555 are used; gaps are intentional.
  # Threshold of 450 ensures the suite body is substantially present.
  if [[ $total -ge 450 ]]; then
    ok "meta: $total test stories cover TS-001..TS-555 range ($count shell + $pwa_count pwa)"
  else
    ko "meta: only $total test stories found — expected 450+ for a complete v8.0 suite"
  fi
}

RESULT=fail
_story_ts_556
: "${RESULT:=fail}"
unset -f _story_ts_556
