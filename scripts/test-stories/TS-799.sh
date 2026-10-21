#!/usr/bin/env bash
# TS-799 — PWA stays connected and quiet through repeated connection flaps
# tags: surface:pwa feature:pwa-connection-stability conflict:pwa-pwa
# pwa-script: pwa/TS-799.mjs
#
# Creates a shell (tmux) session inline, opens its terminal view, then flaps the
# network 6 times (plus one forced WebSocket close) and asserts: no page reload,
# bounded /api/sessions and /api/schedules requests, no ERR_INSUFFICIENT_RESOURCES,
# terminal content preserved, live updates restored, and the reconnect-attempt
# counter not reset between flaps shorter than the stability window.
# See pwa/TS-799.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-799"
story_preflight "surface:pwa feature:pwa-connection-stability conflict:pwa-pwa" || return 0

run_pwa_story "TS-799"
