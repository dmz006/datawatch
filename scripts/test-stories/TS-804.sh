#!/usr/bin/env bash
# TS-804 — empty system shows the Automation empty state with Create Automation
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-804.mjs
# See pwa/TS-804.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-804"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-804"
