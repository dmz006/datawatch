#!/usr/bin/env bash
# TS-642 — Settings Automata tab reachable
# tags: surface:pwa feature:config feature:automata conflict:pwa
# pwa-script: pwa/TS-642.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-642"
story_preflight "surface:pwa feature:config feature:automata conflict:pwa" || return 0
run_pwa_story "TS-642"
