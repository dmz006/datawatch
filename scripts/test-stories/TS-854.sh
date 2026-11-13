#!/usr/bin/env bash
# TS-854 — Projects cards: the shared Cards / Tiles / Compact renderer for the
# Projects view (phone width defaults to compact and is remembered), the
# Unassigned board's Assign / Dismiss / Undo per Automaton, and the removed
# Policy sub-tab (a stored 'policy' value falls back to Overview).
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CURRENT_STORY="TS-854"
story_preflight "surface:pwa feature:projects-cards conflict:pwa-pwa" || return 0

run_pwa_story "projects-cards"
