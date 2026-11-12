#!/usr/bin/env bash
# Alerts header row — the Alerts tab's age text and help link sit on one row to the right at 390 px, and the age ticker updates in place
# tags: surface:pwa feature:alerts-header conflict:pwa-pwa
# pwa-script: pwa/alerts-header-row.mjs
# See pwa/alerts-header-row.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
story_preflight "surface:pwa feature:alerts-header conflict:pwa-pwa" || return 0

run_pwa_story "alerts-header-row"
