#!/usr/bin/env bash
# TS-857 — PWA update loop: an installed web app with an established service worker and no release change makes no automatic navigation, shows no update bar and keeps one versioned worker script
# tags: surface:pwa feature:pwa-update
# pwa-script: pwa/TS-857.mjs
# See pwa/TS-857.mjs header. Never starts or stops a daemon.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-857"
story_preflight "surface:pwa feature:pwa-update" || return 0

run_pwa_story "TS-857"
