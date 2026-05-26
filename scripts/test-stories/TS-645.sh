#!/usr/bin/env bash
# TS-645 — Dashboard stat strip visible
# tags: surface:pwa feature:bootstrap conflict:pwa
# pwa-script: pwa/TS-645.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-645"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0
run_pwa_story "TS-645"
