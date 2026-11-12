#!/usr/bin/env bash
# TS-833 — PWA Settings: the operator-question notification toggles
# tags: surface:pwa feature:settings conflict:config conflict:pwa-pwa
# pwa-script: pwa/TS-833.mjs
#
# Drives autonomous.questions.notify_messaging, notify_alert and return_path
# from the Automata settings tab in a real browser against the sandbox daemon:
# each renders on by default with a translated label, a click turns it off and
# GET /api/config reads false (the other two unchanged, a reload shows it
# unchecked), a second click turns it back on. The original values are
# restored at the end. See pwa/TS-833.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-833"
story_preflight "surface:pwa feature:settings conflict:config conflict:pwa-pwa" || return 0

run_pwa_story "TS-833"
