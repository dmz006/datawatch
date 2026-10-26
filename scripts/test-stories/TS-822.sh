#!/usr/bin/env bash
# TS-822 — PWA Automation tab and attention group: the shared tag browser draws in the Automation tab, draft Automata in the attention group land the view on attention, the viewer's choice overrides the landing rule, a status change and a tag rename update the page through the event stream, and with the stream blocked the poll catches up
# tags: surface:pwa feature:autonomous-tags conflict:pwa
# pwa-script: pwa/TS-822.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-822"
story_preflight "surface:pwa feature:autonomous-tags conflict:pwa" || return 0

run_pwa_story "TS-822"
