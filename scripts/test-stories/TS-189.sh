#!/usr/bin/env bash
# TS-189 — PWA Settings view renders key sections
# tags: surface:pwa feature:pwa conflict:pwa
# pwa-script: pwa/TS-189.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-189"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0

run_pwa_story "TS-189"
