#!/usr/bin/env bash
# TS-806 — slow endpoint shows failed, stale warning only when data is truly old
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-806.mjs
# See pwa/TS-806.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-806"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-806"
