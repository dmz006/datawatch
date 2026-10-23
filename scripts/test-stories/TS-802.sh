#!/usr/bin/env bash
# TS-802 — cold load of every main tab meets the time-to-first-content budget
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-802.mjs
# See pwa/TS-802.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-802"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-802"
