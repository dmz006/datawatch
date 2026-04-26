#!/bin/sh
# BL175 — verify the docs/ → internal/server/web/docs/ sync is well-formed.
#
# Behavior:
#  - If the destination doesn't exist yet (fresh checkout — embedded copy
#    is .gitignored and regenerated at `make build` time), populate it
#    via `make sync-docs` first. This isn't drift; it's the
#    expected-on-CI initial state.
#  - Run rsync dry-run a second time to confirm sync is now idempotent.
#    Any non-trivial diff at that point IS a real bug in the sync logic
#    (skip manifest mismatch, wrong include extensions, etc.) and fails
#    the check.
#
# Used by the pre-commit hook (`hooks/pre-commit-docs-sync`) AND
# `.github/workflows/docs-sync.yaml`. Cheap; runs in seconds.

set -e

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"

# Reuse the same exclude list the Makefile honours.
SKIP=""
if [ -f docs/_embed_skip.txt ]; then
  for line in $(grep -vE '^\s*(#|$)' docs/_embed_skip.txt); do
    SKIP="$SKIP --exclude=$line"
  done
fi

DST="internal/server/web/docs"

# Populate-if-missing — fresh CI checkout doesn't have the dst dir
# (gitignored). This isn't drift; it's the expected initial state.
if [ ! -d "$DST" ]; then
  if command -v make >/dev/null 2>&1; then
    make sync-docs >/dev/null
  else
    mkdir -p "$DST"
    rsync -a --delete \
      --include='*/' --include='*.md' --include='*.png' --include='*.svg' --include='*.jpg' --include='*.gif' --exclude='*' $SKIP \
      docs/ "$DST/" >/dev/null
  fi
fi

# Dry-run; capture lines that would have changed beyond timestamp/perms.
DIFF=$(rsync -ai --dry-run --delete \
  --include='*/' --include='*.md' --include='*.png' --include='*.svg' --include='*.jpg' --include='*.gif' --exclude='*' $SKIP \
  docs/ "$DST/" 2>/dev/null | grep -vE '^\.[df]\.\.t' || true)

if [ -n "$DIFF" ]; then
  echo "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage."
  echo "$DIFF" | head -20
  exit 1
fi

echo "docs/ and $DST in sync."
