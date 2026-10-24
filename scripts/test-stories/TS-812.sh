#!/usr/bin/env bash
# TS-812 — PWA Automata tab: the Progress card renders, shows the failed state with Retry, and carries no tracker IDs
# tags: surface:pwa feature:autonomous-progress conflict:pwa
# pwa-script: pwa/TS-812.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-812"
story_preflight "surface:pwa feature:autonomous-progress conflict:pwa" || return 0

run_pwa_story "TS-812"
