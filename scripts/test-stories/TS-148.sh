#!/usr/bin/env bash
# TS-148 — PWA: Dashboard nav hidden when autonomous disabled (BL313)
# tags: surface:pwa feature:pwa feature:automata conflict:pwa
# pwa-script: pwa/TS-148.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-148"
story_preflight "surface:pwa feature:pwa feature:automata conflict:pwa" || return 0

# API fallback: verify /api/autonomous/config.enabled drives nav button visibility.
# Full browser test via pwa/TS-148.mjs when Node is available.
run_pwa_story "TS-148"
