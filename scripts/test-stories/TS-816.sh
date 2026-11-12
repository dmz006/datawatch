#!/usr/bin/env bash
# TS-816 — websocket bytes on a phone: cold connect, reconnect, idle, show history
# tags: surface:pwa feature:load-performance conflict:pwa-pwa
# pwa-script: pwa/TS-816.mjs
# See pwa/TS-816.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-816"
story_preflight "surface:pwa feature:load-performance conflict:pwa-pwa" || return 0

run_pwa_story "TS-816"
