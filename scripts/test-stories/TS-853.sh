#!/usr/bin/env bash
# TS-853 — the Automaton page's Branch refresh card shows the last result, the commits behind and whether refreshing is stopped by a conflict, renders hostile values as text, posts the override choice (true, then null) to the branch-refresh route, and does not scroll sideways at phone width
# tags: surface:pwa feature:branch-refresh
# pwa-script: pwa/branch-refresh-card.mjs
# See pwa/branch-refresh-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-853"
story_preflight "surface:pwa feature:branch-refresh" || return 0

run_pwa_story "branch-refresh-card"
