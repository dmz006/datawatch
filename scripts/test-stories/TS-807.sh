#!/usr/bin/env bash
# TS-807 — a late older response does not overwrite newer data
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-807.mjs
# See pwa/TS-807.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-807"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-807"
