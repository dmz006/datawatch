#!/bin/sh
# BL175 — verify the embedded PWA docs at internal/server/web/docs/
# are in sync with the source-of-truth at docs/. Run as a pre-commit
# hook (`hooks/pre-commit`) and in CI (`.github/workflows/docs-sync.yaml`).
#
# The check just rsync-dry-runs and fails if any operation would have
# changed the embedded copy. Exits 0 when in sync; non-zero with a
# diff summary when drifted.

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

# Dry-run; capture lines that would have changed.
DIFF=$(rsync -ai --dry-run --delete \
  --include='*/' --include='*.md' --exclude='*' $SKIP \
  docs/ internal/server/web/docs/ 2>/dev/null | grep -vE '^\.[df]\.\.t' || true)

if [ -n "$DIFF" ]; then
  echo "ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage."
  echo "$DIFF" | head -20
  exit 1
fi

echo "docs/ and internal/server/web/docs/ in sync."
