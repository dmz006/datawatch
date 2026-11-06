#!/usr/bin/env bash
# TS-844 — the Observer view's Monitor stats panel shows the Federation — Observer Pull card
# tags: surface:pwa feature:observer-pull group:federation conflict:pwa-pwa
# pwa-script: pwa/observer-pull-card.mjs
# See pwa/observer-pull-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-844"
story_preflight "surface:pwa feature:observer-pull group:federation conflict:pwa-pwa" || return 0

run_pwa_story "observer-pull-card"
