#!/usr/bin/env bash
# TS-826 — PWA task card: wait-cause label and escaped task/board text
# tags: surface:pwa feature:automata conflict:pwa-pwa
# pwa-script: pwa/TS-826.mjs
#
# Renders the waiting-task line, the Automata progress bar and the chat bubble
# header with hostile values. Known wait causes keep their chip label, unknown
# causes (including prototype names) render no chip, and markup in task, board
# and role text shows as text. See pwa/TS-826.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-826"
story_preflight "surface:pwa feature:automata conflict:pwa-pwa" || return 0

run_pwa_story "TS-826"
