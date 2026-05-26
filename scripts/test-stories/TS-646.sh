#!/usr/bin/env bash
# TS-646 — Dark/light theme toggle persists
# tags: surface:pwa feature:config conflict:pwa
# pwa-script: pwa/TS-646.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-646"
story_preflight "surface:pwa feature:config conflict:pwa" || return 0
run_pwa_story "TS-646"
