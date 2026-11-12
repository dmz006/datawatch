#!/usr/bin/env bash
# TS-805 — failing endpoint shows the failed state and Retry recovers
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-805.mjs
# See pwa/TS-805.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-805"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-805"
