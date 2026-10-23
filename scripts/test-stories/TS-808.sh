#!/usr/bin/env bash
# TS-808 — offline then back online recovers
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-808.mjs
# See pwa/TS-808.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-808"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-808"
