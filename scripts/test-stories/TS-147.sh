#!/usr/bin/env bash
# TS-147 — PWA: voice button hidden when whisper not configured (BL314)
# tags: surface:pwa feature:pwa feature:voice conflict:pwa
# pwa-script: pwa/TS-147.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-147"
story_preflight "surface:pwa feature:pwa feature:voice conflict:pwa" || return 0

# API fallback: verify whisper.enabled=false (or absent) means no voice button in active session.
# Full browser test via pwa/TS-147.mjs when Node is available.
run_pwa_story "TS-147"
