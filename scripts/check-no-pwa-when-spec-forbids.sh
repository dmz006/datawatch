#!/usr/bin/env bash
# check-no-pwa-when-spec-forbids.sh — refuse a change set that touches
# internal/server/web/app.js when the task spec forbids a PWA change.
#
# Usage: scripts/check-no-pwa-when-spec-forbids.sh <spec-file> [base-ref]
#
# base-ref defaults to the merge base of HEAD and main. The diff against
# base-ref covers both committed changes and working-tree changes.
#
# Exit codes:
#   0  spec does not forbid a PWA change, or internal/server/web/app.js is untouched
#   1  spec forbids a PWA change and internal/server/web/app.js was changed
#   2  spec file is missing (or the default base-ref could not be determined)
set -uo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <spec-file> [base-ref]" >&2
  exit 2
fi

spec_file=$1

if [ ! -f "$spec_file" ]; then
  echo "error: spec file not found: $spec_file" >&2
  exit 2
fi

spec_text=$(tr '[:upper:]' '[:lower:]' < "$spec_file")

forbidden=0
for phrase in \
  'no pwa change' \
  'no pwa part' \
  'forbade any pwa change' \
  'forbids any pwa change' \
  'must not change the pwa'
do
  case $spec_text in
    *"$phrase"*) forbidden=1 ;;
  esac
done

[ "$forbidden" -eq 1 ] || exit 0

base_ref=${2:-}
if [ -z "$base_ref" ]; then
  if ! base_ref=$(git merge-base HEAD main 2>/dev/null); then
    echo "error: could not determine merge base of HEAD and main" >&2
    exit 2
  fi
fi

if ! changed=$(git diff --name-only "$base_ref" 2>/dev/null); then
  echo "error: git diff --name-only $base_ref failed" >&2
  exit 2
fi

if printf '%s\n' "$changed" | grep -qx 'internal/server/web/app.js'; then
  echo 'forbidden PWA change: internal/server/web/app.js' >&2
  exit 1
fi

exit 0
