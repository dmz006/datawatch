#!/usr/bin/env bash
# TS-854 — the Settings task routing editor edits a row and saves the table, saves a profile, switches the active profile and runs the dry run against a draft Automaton, posts the expected bodies, renders hostile values as text, and does not scroll sideways at phone width
# tags: surface:pwa feature:task-routing
# pwa-script: pwa/task-routing-editor.mjs
# See pwa/task-routing-editor.mjs header; helpers in pwa/perf-lib.mjs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-854"
story_preflight "surface:pwa feature:task-routing" || return 0

run_pwa_story "task-routing-editor"
