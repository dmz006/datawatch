#!/usr/bin/env bash
# TS-823 — session tags: own tags set from the session detail, Automaton-inherited tags shown read-only, and a tag still carried by a session refused on delete with a count
# tags: surface:pwa feature:session-tags conflict:pwa
# pwa-script: pwa/TS-823.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-823"
story_preflight "surface:pwa feature:session-tags conflict:pwa" || return 0

run_pwa_story "TS-823"
