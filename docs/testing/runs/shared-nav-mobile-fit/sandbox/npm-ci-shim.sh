#!/usr/bin/env bash
# Sandbox shim, first on PATH for the probe run only. e2e-pwa.sh runs `npm ci` in
# scripts/test-stories/pwa, whose node_modules is a symlink to
# /home/user/workspace/datawatch (outside the worktree). The probe imports only node
# built-ins, so the install is skipped here and nothing outside the worktree is touched.
if [[ "$1" == "ci" ]]; then
  echo "npm ci skipped by sandbox shim (probe uses node built-ins only)"
  exit 0
fi
exec /home/user/.nvm/versions/node/v24.15.0/bin/npm "$@"
