#!/usr/bin/env bash
# TS-850 — at phone widths (320, 390, 430) the Automaton detail view's eight-tab strip scrolls inside itself, the tapped tab is active and revealed, the tab body changes, and the page never scrolls sideways, including across the Automation sub-tabs and the bottom nav
# tags: surface:pwa feature:automaton-detail-tabs conflict:pwa-pwa
# pwa-script: pwa/automaton-detail-tabs-mobile.mjs
# See pwa/automaton-detail-tabs-mobile.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-850"
story_preflight "surface:pwa feature:automaton-detail-tabs conflict:pwa-pwa" || return 0

run_pwa_story "automaton-detail-tabs-mobile"
