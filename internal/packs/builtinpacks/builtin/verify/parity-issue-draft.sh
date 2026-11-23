#!/usr/bin/env bash
# Verification for the parity-issue-draft standard task.
# The task writes one issue-draft file under docs/plans/parity-issues/
# and never touches a tracker; check the draft file exists.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)" || exit 1
fail=0
say() { printf '%s\n' "$*"; }
if [ ! -d docs/plans/parity-issues ]; then
  say "missing folder: docs/plans/parity-issues"
  fail=1
elif ! find docs/plans/parity-issues -maxdepth 1 -type f -name '*.md' | grep -q .; then
  say "no issue-draft file under docs/plans/parity-issues"
  fail=1
fi
exit $fail
