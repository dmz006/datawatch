#!/usr/bin/env bash
# TS-798 — PWA terminal view live-updates before and after a WebSocket reconnect
# tags: surface:pwa feature:terminal-live-update conflict:pwa-pwa
# pwa-script: pwa/TS-798.mjs
#
# Creates a shell (tmux) session inline, types commands through the on-screen
# input and asserts the output appears in the terminal within 3000 ms, both
# before and after a forced WebSocket close. See pwa/TS-798.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-798"
story_preflight "surface:pwa feature:terminal-live-update conflict:pwa-pwa" || return 0

run_pwa_story "TS-798"
