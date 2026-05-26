#!/usr/bin/env bash
# TS-637 — Sessions view renders list
# tags: surface:pwa feature:sessions conflict:pwa
# pwa-script: pwa/TS-637.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-637"
story_preflight "surface:pwa feature:sessions conflict:pwa" || return 0
run_pwa_story "TS-637"
