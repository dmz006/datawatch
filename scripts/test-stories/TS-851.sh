#!/usr/bin/env bash
# TS-851 — Settings, General shows the Git hosts card: each host renders its name, kind, base URL, credential source and vault secret name fields, and no token value ever appears in the card
# tags: surface:pwa feature:git-hosts conflict:pwa-pwa
# pwa-script: pwa/git-hosts-card.mjs
# See pwa/git-hosts-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-851"
story_preflight "surface:pwa feature:git-hosts conflict:pwa-pwa" || return 0

run_pwa_story "git-hosts-card"
