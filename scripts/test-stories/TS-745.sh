#!/usr/bin/env bash
# TS-745 — PWA: select-all (All/None button) operates on _visibleDone filtered set
# tags: surface:pwa feature:pwa conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-745"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0
run_pwa_story "TS-745"
