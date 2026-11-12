#!/usr/bin/env bash
# TS-834 — PWA Automation: the operator-question channel (off, on, withdraw)
# tags: surface:pwa feature:questions conflict:config conflict:pwa-pwa
# pwa-script: pwa/TS-834.mjs
#
# Drives the operator-question channel from the Automation view in a real
# browser against the sandbox daemon: with autonomous.questions.enabled off
# the card shows the disabled text with nothing listed and POST/GET
# /api/questions answer 503; with it on, one question is raised (run-unique
# text, two options, a recommendation, a default, label "information") and
# lands in the queue card, and opening it shows the options, the
# recommendation and the default; then it is withdrawn (the store's removal
# — an answer would be a decision of record) and the history shows it
# withdrawn while the open queue no longer lists it. The switch is restored
# in the story's finally. See pwa/TS-834.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-834"
story_preflight "surface:pwa feature:questions conflict:config conflict:pwa-pwa" || return 0

run_pwa_story "TS-834"
