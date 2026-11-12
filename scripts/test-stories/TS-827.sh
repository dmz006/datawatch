#!/usr/bin/env bash
# TS-827 — PWA Settings: the "Operator questions for Automata" toggle
# tags: surface:pwa feature:settings conflict:config conflict:pwa-pwa
# pwa-script: pwa/TS-827.mjs
#
# Drives the autonomous.questions.enabled switch from the Automata settings tab
# in a real browser against the sandbox daemon: default off, a click turns it
# on and GET /api/config reads true, /api/questions stops answering 503, a
# second click turns it off again. The original value is restored at the end.
# See pwa/TS-827.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-827"
story_preflight "surface:pwa feature:settings conflict:config conflict:pwa-pwa" || return 0

run_pwa_story "TS-827"
