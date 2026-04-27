#!/usr/bin/env bash
# Asset retention — keep binaries/containers for every MAJOR (X.0.0)
# plus the latest MINOR (highest X.Y.0) plus the latest PATCH on the
# latest minor (highest X.Y.Z where Z > 0 within the latest minor's
# X.Y series). Everything else gets its release-page assets deleted.
#
# Container images on GHCR aren't in scope here (need a separate
# read:packages + delete:packages token); per AGENT.md retention
# rule they follow the same keep-set pattern.

set -uo pipefail

DRY_RUN="${DRY_RUN:-0}"
total_deletes=0
total_releases=0

# 1. Collect every release tag.
mapfile -t all_tags < <(gh release list --limit 500 2>/dev/null \
  | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sort -uV)

# 2. Build the keep-set.
keep=()

# 2a. Every major (X.0.0).
for t in "${all_tags[@]}"; do
  if [[ "$t" =~ ^v[0-9]+\.0\.0$ ]]; then
    keep+=("$t")
  fi
done

# 2b. Latest minor — highest X.Y.0 (Y >= 1) across the whole list.
latest_minor=""
for t in "${all_tags[@]}"; do
  if [[ "$t" =~ ^v[0-9]+\.[0-9]+\.0$ ]] && [[ ! "$t" =~ ^v[0-9]+\.0\.0$ ]]; then
    latest_minor="$t"
  fi
done
if [[ -n "$latest_minor" ]]; then
  keep+=("$latest_minor")
fi

# 2c. Latest patch on the latest minor — highest X.Y.Z (Z > 0) where
#     X.Y matches the latest minor's X.Y. Skipped if no such patch
#     exists yet.
if [[ -n "$latest_minor" ]]; then
  xy_prefix="${latest_minor%.0}."   # e.g. "v5.25."
  latest_patch=""
  for t in "${all_tags[@]}"; do
    if [[ "$t" == "$xy_prefix"* && "$t" != "$latest_minor" ]]; then
      latest_patch="$t"
    fi
  done
  if [[ -n "$latest_patch" ]]; then
    keep+=("$latest_patch")
  fi
fi

# Dedup + announce.
keep_unique=$(printf '%s\n' "${keep[@]}" | sort -u | tr '\n' ' ')
echo "[retention] keep set: ${keep_unique}"

# 3. Delete every release's assets unless it's in the keep-set.
for tag in "${all_tags[@]}"; do
  if printf '%s\n' "${keep[@]}" | grep -qx "$tag"; then
    continue
  fi
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
done

echo "[summary] processed $total_releases releases, deleted $total_deletes assets"
