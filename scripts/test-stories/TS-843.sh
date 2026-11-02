#!/usr/bin/env bash
# TS-843 — the burn guard card on the usage calculator view and its Monitor stats card show the guard's state
# tags: surface:pwa feature:burn-guard group:usage conflict:pwa-pwa
# pwa-script: pwa/burn-guard-card.mjs
# See pwa/burn-guard-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-843"
story_preflight "surface:pwa feature:burn-guard group:usage conflict:pwa-pwa" || return 0

run_pwa_story "burn-guard-card"
