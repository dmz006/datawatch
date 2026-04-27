#!/usr/bin/env bash
# Delete every release asset on every non-major release.
# Major = X.0.0 (only v1.0.0 v2.0.0 v3.0.0 v4.0.0 v5.0.0 retain assets).

set -uo pipefail

DRY_RUN="${DRY_RUN:-0}"
total_deletes=0
total_releases=0

while IFS= read -r tag; do
  total_releases=$((total_releases + 1))
  assets=$(gh release view "$tag" --json assets --jq '.assets[].name' 2>/dev/null)
  if [[ -z "$assets" ]]; then
    continue
  fi
  while IFS= read -r asset; do
    if [[ "$DRY_RUN" == "1" ]]; then
      echo "[dry-run] gh release delete-asset $tag $asset"
    else
      gh release delete-asset "$tag" "$asset" --yes 2>&1 | head -1
    fi
    total_deletes=$((total_deletes + 1))
  done <<< "$assets"
done < <(gh release list --limit 200 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sort -uV | grep -vE '^v[0-9]+\.0\.0$' | grep -v '^v5.22.0$')

echo "[summary] processed $total_releases releases, deleted $total_deletes assets"
