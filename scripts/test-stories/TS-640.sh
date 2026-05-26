#!/usr/bin/env bash
# TS-640 — Settings LLM tab: backend list visible
# tags: surface:pwa feature:config conflict:pwa
# pwa-script: pwa/TS-640.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-640"
story_preflight "surface:pwa feature:config conflict:pwa" || return 0
run_pwa_story "TS-640"
