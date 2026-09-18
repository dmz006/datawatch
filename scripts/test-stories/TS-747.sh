#!/usr/bin/env bash
# TS-747 — PWA: apiFetch 204/205 returns null (no "Unexpected end of JSON input")
# tags: surface:pwa feature:pwa conflict:pwa parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-747"
story_preflight "surface:pwa feature:pwa conflict:pwa" || return 0
run_pwa_story "TS-747"
