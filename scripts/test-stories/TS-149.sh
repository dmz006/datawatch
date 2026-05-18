#!/usr/bin/env bash
# TS-149 — PWA: fullscreen toggle button present + install prompt wired (BL315)
# tags: surface:pwa feature:pwa conflict:pwa
# pwa-script: pwa/TS-149.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-149"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0

run_pwa_story "TS-149"
