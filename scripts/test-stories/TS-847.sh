#!/usr/bin/env bash
# TS-847 — the Merge pipeline card on the Automation Overview shows the pipeline in effect for the chosen or preselected project, or the no-project empty state with the project choice and a clickable worktree_base_repo help link
# tags: surface:pwa feature:merge-pipeline conflict:pwa-pwa
# pwa-script: pwa/merge-pipeline-card.mjs
# See pwa/merge-pipeline-card.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-847"
story_preflight "surface:pwa feature:merge-pipeline conflict:pwa-pwa" || return 0

run_pwa_story "merge-pipeline-card"
