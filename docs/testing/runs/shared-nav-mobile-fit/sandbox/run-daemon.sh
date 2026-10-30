#!/usr/bin/env bash
# Sandbox daemon launcher for the shared-nav-mobile-fit probe run.
# e2e-pwa.sh deletes its per-run dir (and daemon.log) after a passing run, so the
# daemon's output is appended here instead. exec keeps the PID e2e-pwa.sh tracks.
# TMUX_TMPDIR is set here, inside the worktree, so the daemon never falls back to the
# production tmux socket when the run is started with TMUX_TMPDIR unset.
export TMUX_TMPDIR=/home/user/.datawatch/prd-worktrees/b54f0001/bin/sandbox/tmux
mkdir -p "$TMUX_TMPDIR"
exec /home/user/.datawatch/prd-worktrees/b54f0001/bin/datawatch "$@" \
  >> /home/user/.datawatch/prd-worktrees/b54f0001/bin/sandbox/daemon.log 2>&1
