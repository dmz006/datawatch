#!/usr/bin/env bash
# TS-641 — Settings Comms tab reachable
# tags: surface:pwa feature:config conflict:pwa
# pwa-script: pwa/TS-641.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-641"
story_preflight "surface:pwa feature:config conflict:pwa" || return 0
run_pwa_story "TS-641"
