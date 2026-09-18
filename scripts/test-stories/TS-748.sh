#!/usr/bin/env bash
# TS-748 — PWA: autonomous task row elements (session link, error panel, retry btn, verif)
# tags: surface:pwa feature:automata conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-748"
story_preflight "surface:pwa feature:automata conflict:pwa" || return 0
run_pwa_story "TS-748"
