#!/usr/bin/env bash
# Container retention on GHCR — counterpart to delete-past-minor-assets.sh
# but for container images on the GitHub Container Registry. Same keep-set
# logic: every MAJOR (X.0.0) plus the latest MINOR (highest X.Y.0) plus
# the latest PATCH on the latest minor (highest X.Y.Z where Z > 0 in the
# X.Y series).
#
# Requires a GitHub token with `read:packages + delete:packages` scope
# (default `gh auth login` token only has read; mint a fine-grained PAT).
# Export it as GITHUB_TOKEN before running, or pass via gh CLI:
#   GITHUB_TOKEN=<pat> ./scripts/delete-past-minor-containers.sh
#
# Set DRY_RUN=1 to preview without deleting.

set -uo pipefail

DRY_RUN="${DRY_RUN:-0}"
OWNER="dmz006"
PACKAGES=(
  "datawatch-parent-full"
  "datawatch-agent-base"
  "datawatch-agent-claude"
  "datawatch-agent-opencode"
  "datawatch-agent-aider"
  "datawatch-agent-gemini"
  "datawatch-agent-goose"
  "datawatch-stats-cluster"
)

total_deletes=0
total_versions=0

# 1. Collect every release tag (same source of truth as the assets script).
mapfile -t all_tags < <(gh release list --limit 500 2>/dev/null \
  | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sort -uV)

# 2. Build the keep-set (same algorithm as delete-past-minor-assets.sh).
keep=()
for t in "${all_tags[@]}"; do
  if [[ "$t" =~ ^v[0-9]+\.0\.0$ ]]; then keep+=("$t"); fi
done
latest_minor=""
for t in "${all_tags[@]}"; do
  if [[ "$t" =~ ^v[0-9]+\.[0-9]+\.0$ ]] && [[ ! "$t" =~ ^v[0-9]+\.0\.0$ ]]; then
    latest_minor="$t"
  fi
done
if [[ -n "$latest_minor" ]]; then
  keep+=("$latest_minor")
  xy_prefix="${latest_minor%.0}."
  latest_patch=""
  for t in "${all_tags[@]}"; do
    if [[ "$t" == "$xy_prefix"* && "$t" != "$latest_minor" ]]; then
      latest_patch="$t"
    fi
  done
  [[ -n "$latest_patch" ]] && keep+=("$latest_patch")
fi
keep_unique=$(printf '%s\n' "${keep[@]}" | sort -u | tr '\n' ' ')
echo "[retention] keep set: ${keep_unique}"

# 3. For each package, list versions and delete any whose tag isn't in keep-set.
for pkg in "${PACKAGES[@]}"; do
  echo "[pkg] $pkg"
  versions_json=$(gh api -H 'Accept: application/vnd.github+json' \
    "/users/${OWNER}/packages/container/${pkg}/versions?per_page=100" 2>/dev/null)
  if [[ -z "$versions_json" || "$versions_json" == "null" ]]; then
    echo "  (no versions or no access — does the token have read:packages?)"
    continue
  fi
  echo "$versions_json" | python3 -c "
import json, sys
data = json.load(sys.stdin)
for v in data:
    vid = v.get('id')
    tags = v.get('metadata', {}).get('container', {}).get('tags', [])
    print(f\"{vid}\\t{','.join(tags) if tags else '<untagged>'}\")
" | while IFS=$'\t' read -r vid tags; do
    total_versions=$((total_versions + 1))
    keep_this=0
    for t in $(echo "$tags" | tr ',' ' '); do
      for k in "${keep[@]}"; do
        if [[ "$t" == "$k" || "$t" == "${k#v}" ]]; then keep_this=1; break; fi
      done
      [[ $keep_this == 1 ]] && break
    done
    if [[ "$tags" == "<untagged>" ]]; then
      keep_this=0  # always prune dangling layers
    fi
    if [[ $keep_this == 1 ]]; then
      continue
    fi
    if [[ "$DRY_RUN" == "1" ]]; then
      echo "  [dry-run] would delete version $vid (tags: $tags)"
    else
      gh api -X DELETE "/users/${OWNER}/packages/container/${pkg}/versions/$vid" 2>&1 | head -1
    fi
    total_deletes=$((total_deletes + 1))
  done
done

echo "[summary] processed $total_versions versions across ${#PACKAGES[@]} packages, deleted $total_deletes"
