#!/usr/bin/env bash
# TS-840 — the Supervisor watch card on the Automation view shows the mode, the last poll, the open findings and the comparison
# tags: surface:pwa feature:supervisor-watch conflict:pwa-pwa
# pwa-script: pwa/supervisor-watch-card.mjs
# See pwa/supervisor-watch-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-840"
story_preflight "surface:pwa feature:supervisor-watch conflict:pwa-pwa" || return 0

run_pwa_story "supervisor-watch-card"
