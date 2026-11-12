#!/usr/bin/env bash
# TS-797 — PWA schedule-load flood stays bounded after a WS reconnect (20+ sessions)
# tags: surface:pwa feature:schedules conflict:pwa-pwa
# pwa-script: pwa/TS-797.mjs
#
# Verifies the /api/schedules request-flood fix (app.js applySessionUpdate /
# _loadSessionSchedules coalescer) against an untrusted self-signed origin and
# a trusted origin (TEST_TRUSTED_TLS or host-generated CA + spki-list), with
# ≥20 seeded sessions (always cleaned up). See pwa/TS-797.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-797"
story_preflight "surface:pwa feature:schedules conflict:pwa-pwa" || return 0

run_pwa_story "TS-797"
