#!/usr/bin/env bash
# TS-746 — PWA: filter chip change clears active selection
# tags: surface:pwa feature:pwa conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-746"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0
run_pwa_story "TS-746"
