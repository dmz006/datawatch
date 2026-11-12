#!/usr/bin/env bash
# TS-810 — three reloads in a row each within budget
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-810.mjs
# See pwa/TS-810.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-810"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-810"
