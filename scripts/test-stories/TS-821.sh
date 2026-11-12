#!/usr/bin/env bash
# TS-821 — Automata tab tag browser: a three-level tag tree with subtree counts, an Automaton under two children counted once, the "also in:" line, a primary tag that changes only the order, ANY then ALL matching, the flat grid with the no-matches line while filtered, and the phone-width drill-down with breadcrumb, back row and hash
# tags: surface:pwa feature:automata-tag-browser conflict:pwa
# pwa-script: pwa/TS-821.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-821"
story_preflight "surface:pwa feature:automata-tag-browser conflict:pwa" || return 0

run_pwa_story "TS-821"
