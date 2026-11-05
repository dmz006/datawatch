#!/usr/bin/env bash
# TS-849 — the Supervisor loop card on the Automation view shows the jobs with their last outcome and the recent runs
# tags: surface:pwa feature:supervisor-loop conflict:pwa-pwa
# pwa-script: pwa/supervisor-loop-card.mjs
# See pwa/supervisor-loop-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-849"
story_preflight "surface:pwa feature:supervisor-loop conflict:pwa-pwa" || return 0

run_pwa_story "supervisor-loop-card"
