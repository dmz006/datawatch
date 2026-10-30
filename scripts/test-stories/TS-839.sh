#!/usr/bin/env bash
# TS-839 — PWA Automation sub-tab: the security failure card renders each report read-only
# tags: surface:pwa feature:automata conflict:pwa-pwa
# pwa-script: pwa/TS-839.mjs
#
# The Automation sub-tab's security failure card draws one read-only card per
# GET /api/autonomous/security-failures report beside the triage card: the
# dependency, advisory, publication date, registry state, recommendation, mode
# and the action taken (or the shadow would-have-taken line), every value as
# text, a phone-width fit and the empty state. Rendered in a real browser with
# the route fulfilled by the story; the daemon stores and changes nothing.
# See pwa/TS-839.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-839"
story_preflight "surface:pwa feature:automata conflict:pwa-pwa" || return 0

run_pwa_story "TS-839"
