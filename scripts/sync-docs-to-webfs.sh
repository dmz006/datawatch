#!/usr/bin/env bash
#
# sync-docs-to-webfs.sh — mirror the canonical docs/datawatch-definitions.md
# into the embedded web FS so the daemon serves it at /docs/.
#
# Operator-directed 2026-05-05 (BL273): the help icons in the PWA point at
# /docs/datawatch-definitions.md. The Go //go:embed directive at
# internal/server/server.go can only embed files under internal/server/web/,
# so the canonical doc must be mirrored there at build time.
#
# Idempotent. Run before every build / release-smoke. Fails the script if
# the destination is staler than the source — release-smoke uses --check
# mode to surface the gap without writing.

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
SRC="$ROOT/docs/datawatch-definitions.md"
DST_DIR="$ROOT/internal/server/web/docs"
DST="$DST_DIR/datawatch-definitions.md"

CHECK=0
if [ "${1:-}" = "--check" ]; then
  CHECK=1
fi

if [ ! -f "$SRC" ]; then
  echo "ERR: canonical doc not found at $SRC" >&2
  exit 2
fi

mkdir -p "$DST_DIR"

if [ -f "$DST" ] && cmp -s "$SRC" "$DST"; then
  exit 0  # already in sync
fi

if [ $CHECK -eq 1 ]; then
  echo "ERR: $DST is stale (out of sync with $SRC)." >&2
  echo "     Run: scripts/sync-docs-to-webfs.sh" >&2
  exit 1
fi

cp "$SRC" "$DST"
echo "synced docs/datawatch-definitions.md → internal/server/web/docs/"
