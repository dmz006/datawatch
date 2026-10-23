#!/usr/bin/env bash
# TS-803 — large seeded set loads within budget on slow-4G
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-803.mjs
# See pwa/TS-803.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-803"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-803"
