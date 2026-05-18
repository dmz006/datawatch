#!/usr/bin/env bash
# TS-130 — PWA loads, splash resolves, all nav views reachable (Playwright visual test)
# tags: surface:pwa feature:pwa conflict:pwa
# pwa-script: pwa/TS-130.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-130"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0

run_pwa_story "TS-130"
