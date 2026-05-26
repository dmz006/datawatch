#!/usr/bin/env bash
# TS-638 — Alerts view renders
# tags: surface:pwa feature:alerts conflict:pwa
# pwa-script: pwa/TS-638.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-638"
story_preflight "surface:pwa feature:alerts conflict:pwa" || return 0
run_pwa_story "TS-638"
