#!/usr/bin/env bash
# Sandbox daemon launcher for the shared-nav-mobile-fit probe run.
# e2e-pwa.sh deletes its per-run dir (and daemon.log) after a passing run, so the
# daemon's output is appended here instead. exec keeps the PID e2e-pwa.sh tracks.
exec /home/dmz/.datawatch/prd-worktrees/b54f0001/bin/datawatch "$@" \
  >> /home/dmz/.datawatch/prd-worktrees/b54f0001/bin/sandbox/daemon.log 2>&1
