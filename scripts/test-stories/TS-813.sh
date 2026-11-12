#!/usr/bin/env bash
# TS-813 — Automata tab bar fits at 320 px
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-813.mjs
# See pwa/TS-813.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-813"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-813"
