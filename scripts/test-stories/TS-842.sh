#!/usr/bin/env bash
# TS-842 — the Lane fill card on the Automation Overview shows the mode, the per-lane-kind modes, the lane counts and a refusal chip on each blocked decision
# tags: surface:pwa feature:lane-fill conflict:pwa-pwa
# pwa-script: pwa/lane-fill-card.mjs
# See pwa/lane-fill-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-842"
story_preflight "surface:pwa feature:lane-fill conflict:pwa-pwa" || return 0

run_pwa_story "lane-fill-card"
