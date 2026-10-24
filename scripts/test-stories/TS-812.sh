#!/usr/bin/env bash
# TS-812 — reconnect after a dropped connection stays within a small request and byte budget
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-812.mjs
# See pwa/TS-812.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-812"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-812"
