#!/usr/bin/env bash
# TS-643 — Observer view loads without error
# tags: surface:pwa feature:observer conflict:pwa
# pwa-script: pwa/TS-643.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-643"
story_preflight "surface:pwa feature:observer conflict:pwa" || return 0
run_pwa_story "TS-643"
