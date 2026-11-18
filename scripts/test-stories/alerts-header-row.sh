#!/usr/bin/env bash
# Alerts header row — the Alerts tab's state text and help link sit on one row at 390 px with no overflow, and no ticking "last updated" age text
# tags: surface:pwa feature:alerts-header conflict:pwa-pwa
# pwa-script: pwa/alerts-header-row.mjs
# See pwa/alerts-header-row.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
story_preflight "surface:pwa feature:alerts-header conflict:pwa-pwa" || return 0

run_pwa_story "alerts-header-row"
