#!/usr/bin/env bash
# TS-838 — the Automaton decision log shows an answer id as a link that opens the verbatim quote
# tags: surface:pwa feature:questions conflict:pwa-pwa
# pwa-script: pwa/question-decision-link.mjs
# See pwa/question-decision-link.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-838"
story_preflight "surface:pwa feature:questions conflict:pwa-pwa" || return 0

run_pwa_story "question-decision-link"
