#!/usr/bin/env bash
# TS-802 — Observer mobile-fit regression guard: replays the recorded unfixed-tree
#         scenario set (mobile-fit-observer-regressions.json, 68 finding groups,
#         29,968 findings) and FAILS while any recorded group id still produces
#         findings. MUST fail on the unfixed tree (that is its job); after the
#         Observer / System Statistics layout fix it must pass with none of the
#         68 groups reproducing. Re-emits the mobile-fit report to
#         $EVIDENCE_DIR/TS-802/. Companion: pwa/TS-803.mjs (TS-803) is the
#         desktop/tablet regression guard for the same fix.
# tags: surface:pwa feature:pwa-observer-mobile-fit conflict:pwa-pwa
# pwa-script: pwa/TS-802.mjs
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-802"
story_preflight "surface:pwa feature:pwa-observer-mobile-fit conflict:pwa-pwa" || return 0
run_pwa_story "TS-802"
