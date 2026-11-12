#!/usr/bin/env bash
# TS-846 — PWA multi-server: selecting a federated peer from the header chip, the server picker modal and the Settings servers list switches the whole app to it
# tags: surface:pwa feature:multi-server
# pwa-script: pwa/TS-846.mjs
#
# Starts two sandbox daemons on their own free TLS test ports (never the
# production daemon or the operator config), registers the second as the
# federated peer "ralfthewise", and drives the first in a real browser. From
# each control that can select a server it checks that state.activeServer, the
# toast, the server header line, the About page and every Settings tab show the
# peer, that selecting the peer again changes nothing, and that the explicit
# back-to-local control returns to the first daemon. Both daemons are ended on
# every exit path. See pwa/TS-846.mjs header.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-846"
story_preflight "surface:pwa feature:multi-server" || return 0

# Run the browser script without the operator's tmux environment, then restore it.
_ts846_tmux="${TMUX-}"; _ts846_tmux_set="${TMUX+x}"
_ts846_tmpdir="${TMUX_TMPDIR-}"; _ts846_tmpdir_set="${TMUX_TMPDIR+x}"
unset TMUX TMUX_TMPDIR
run_pwa_story "TS-846"
[[ -n "$_ts846_tmux_set" ]] && export TMUX="$_ts846_tmux"
[[ -n "$_ts846_tmpdir_set" ]] && export TMUX_TMPDIR="$_ts846_tmpdir"
unset _ts846_tmux _ts846_tmux_set _ts846_tmpdir _ts846_tmpdir_set
