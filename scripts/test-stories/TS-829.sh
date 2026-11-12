#!/usr/bin/env bash
# TS-829 — PWA Settings: the "Stuck Automaton timeout" field
# tags: surface:pwa feature:settings conflict:config conflict:pwa-pwa
# pwa-script: pwa/TS-829.mjs
#
# Drives autonomous.stuck_running_seconds from the Automata settings tab in a
# real browser against the sandbox daemon: the field renders with its label,
# the 900 placeholder and its help text, typing a value writes it so that
# GET /api/config reads it back, and clearing it reads back 0 (the default
# applies). The original value is restored at the end.
# See pwa/TS-829.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-829"
story_preflight "surface:pwa feature:settings conflict:config conflict:pwa-pwa" || return 0

run_pwa_story "TS-829"
