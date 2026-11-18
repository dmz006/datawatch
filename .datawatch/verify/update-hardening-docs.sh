#!/usr/bin/env bash
# Verify the documentation pass for the hardened paths.
# Runs make sync-docs first (when available) so the embedded docs copy
# matches the source docs, then checks the cumulative changed set.
# Artefact dirs and CHANGELOG.md are excluded by dw_changed_set
# (artefact_files_allowed).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

# 1. docs sync first.
dw_sync_docs

# 2. cumulative changed set vs merge base must contain at least one docs/
#    file outside the generated artefact dirs (excluded by dw_changed_set).
changed="$(dw_changed_set)"
docs="$(printf '%s\n' "$changed" | grep -E -- '^docs/' || true)"
if [ -z "$docs" ]; then
  echo "FAIL: no docs/ file in the cumulative changed set for the documentation pass"
  exit 1
fi
while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || { echo "FAIL: docs file $f is not on disk"; exit 1; }
  [ -s "$f" ] || { echo "FAIL: docs file $f is empty"; exit 1; }
  case "$f" in
    *.md) ;;
    *) echo "FAIL: docs file $f is not markdown"; exit 1 ;;
  esac
  echo "OK: docs file $f present and non-empty"
done <<EOF
$docs
EOF

echo "PASS: update-hardening-docs"
