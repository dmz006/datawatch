#!/usr/bin/env bash
# TS-845 — PWA About page: a federated peer on a different release shows a version-skew note, no update banner and no reload
# tags: surface:pwa feature:multi-server
# pwa-script: pwa/TS-845.mjs
#
# Drives the sandbox daemon in a real browser: selects a federated peer whose
# release differs from the primary, opens Settings then About, checks that the
# version-skew note appears and that no update banner appears, switches back to
# the primary and checks the page did not reload. Never starts or stops a
# daemon; refuses the production ports 8080 and 8443. See pwa/TS-845.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-845"
story_preflight "surface:pwa feature:multi-server" || return 0

# Run the browser script without the operator's tmux environment, then restore it.
_ts845_tmux="${TMUX-}"; _ts845_tmux_set="${TMUX+x}"
_ts845_tmpdir="${TMUX_TMPDIR-}"; _ts845_tmpdir_set="${TMUX_TMPDIR+x}"
unset TMUX TMUX_TMPDIR
run_pwa_story "TS-845"
[[ -n "$_ts845_tmux_set" ]] && export TMUX="$_ts845_tmux"
[[ -n "$_ts845_tmpdir_set" ]] && export TMUX_TMPDIR="$_ts845_tmpdir"
unset _ts845_tmux _ts845_tmux_set _ts845_tmpdir _ts845_tmpdir_set
