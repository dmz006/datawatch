#!/usr/bin/env bash
# TS-829 — PWA Settings: the Automata "Descendant stall timeout" field
# tags: surface:pwa feature:settings conflict:config conflict:pwa-pwa
# pwa-script: pwa/TS-829.mjs
#
# Edits autonomous.descendant_stall_seconds from the Automata settings tab in a
# real browser against the sandbox daemon: the field renders with its
# translated label and the default placeholder, a typed value is saved and
# GET /api/config reads it back, and the card shows it after a reload. The
# original value is restored at the end. See pwa/TS-829.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-829"
story_preflight "surface:pwa feature:settings conflict:config conflict:pwa-pwa" || return 0

run_pwa_story "TS-829"
