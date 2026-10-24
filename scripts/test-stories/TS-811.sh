#!/usr/bin/env bash
# TS-811 — first tab loads request summary lists within a byte budget
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-811.mjs
# See pwa/TS-811.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-811"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-811"
