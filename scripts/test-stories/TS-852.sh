#!/usr/bin/env bash
# TS-852 — at a 390 px viewport nothing (peer-stale badge or connectivity alert) sits over the Settings nav button label
# tags: surface:pwa feature:settings-nav conflict:pwa-pwa
# pwa-script: pwa/settings-nav-clear-of-peer-alert.mjs
# See pwa/settings-nav-clear-of-peer-alert.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-852"
story_preflight "surface:pwa feature:settings-nav conflict:pwa-pwa" || return 0

run_pwa_story "settings-nav-clear-of-peer-alert"
