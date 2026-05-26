#!/usr/bin/env bash
# TS-644 — Autonomous/Automata view renders
# tags: surface:pwa feature:automata conflict:pwa
# pwa-script: pwa/TS-644.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-644"
story_preflight "surface:pwa feature:automata conflict:pwa" || return 0
run_pwa_story "TS-644"
