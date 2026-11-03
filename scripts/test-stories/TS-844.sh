#!/usr/bin/env bash
# TS-844 — PWA Sessions list: the Stalled state chip, localized and filtering
# tags: surface:pwa feature:sessions
# pwa-script: pwa/TS-844.mjs
#
# Drives the sandbox daemon in a real browser: opens the Sessions list with a
# session in the stalled state, checks the Stalled chip renders its localized
# label (session_chip_stalled), that the label changes when the UI language is
# switched to another locale, and that clicking the chip filters the list to
# sessions in the stalled state. Never starts or stops a daemon; refuses the
# production ports 8080 and 8443. See pwa/TS-844.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-844"
story_preflight "surface:pwa feature:sessions" || return 0

# Run the browser script without the operator's tmux environment, then restore it.
_ts844_tmux="${TMUX-}"; _ts844_tmux_set="${TMUX+x}"
_ts844_tmpdir="${TMUX_TMPDIR-}"; _ts844_tmpdir_set="${TMUX_TMPDIR+x}"
unset TMUX TMUX_TMPDIR
run_pwa_story "TS-844"
[[ -n "$_ts844_tmux_set" ]] && export TMUX="$_ts844_tmux"
[[ -n "$_ts844_tmpdir_set" ]] && export TMUX_TMPDIR="$_ts844_tmpdir"
unset _ts844_tmux _ts844_tmux_set _ts844_tmpdir _ts844_tmpdir_set
