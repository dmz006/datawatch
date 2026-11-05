#!/usr/bin/env bash
# TS-848 — the Trace card shows coverage, roll-up and timeline, filters to errors, and says so when tracing is off
# tags: surface:pwa feature:agent-tracing conflict:pwa-pwa
# pwa-script: pwa/trace-card.mjs
# See pwa/trace-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-848"
story_preflight "surface:pwa feature:agent-tracing conflict:pwa-pwa" || return 0

run_pwa_story "trace-card"
