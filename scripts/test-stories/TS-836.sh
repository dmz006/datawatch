#!/usr/bin/env bash
# TS-836 — PWA Automation detail: pending-insert rows + the waiting count
# tags: surface:pwa feature:automata conflict:pwa-pwa
# pwa-script: pwa/TS-836.mjs
#
# The Automation detail page renders each held insert's member id, PRD id,
# requester and reason as escaped visible text and shows an Approve control
# on editable statuses, and the loop-status row words "waiting: N" from the
# daemon-side pending_members count — parity with what the CLI prints for the
# same pending inserts. Rendered in a real browser against the sandbox daemon.
# No state is created or mutated. See pwa/TS-836.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-836"
story_preflight "surface:pwa feature:automata conflict:pwa-pwa" || return 0

run_pwa_story "TS-836"
