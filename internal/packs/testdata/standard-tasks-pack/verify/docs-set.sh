#!/usr/bin/env bash
# Verification for the docs-set standard task.
# The task brings the affected howto, operations and glossary entries
# into shape; check the doc index and the core doc files exist and are
# non-empty.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)" || exit 1
fail=0
say() { printf '%s\n' "$*"; }
for f in docs/README.md docs/operations.md docs/datawatch-definitions.md; do
  if [ ! -s "$f" ]; then
    say "missing or empty file: $f"
    fail=1
  fi
done
exit $fail
