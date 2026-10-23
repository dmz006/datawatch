#!/usr/bin/env bash
# TS-809 — phone and tablet widths: no overflow, indicators visible
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-809.mjs
# See pwa/TS-809.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-809"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-809"
