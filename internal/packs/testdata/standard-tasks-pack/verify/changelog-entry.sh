#!/usr/bin/env bash
# Verification for the changelog-entry standard task.
# The task writes one changelog entry; check the changelog exists and
# carries at least one entry heading.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)" || exit 1
fail=0
say() { printf '%s\n' "$*"; }
if [ ! -f CHANGELOG.md ]; then
  say "missing file: CHANGELOG.md"
  fail=1
elif ! grep -qE '^(\s*[#]|\[)' CHANGELOG.md; then
  say "CHANGELOG.md has no entry heading"
  fail=1
fi
exit $fail
