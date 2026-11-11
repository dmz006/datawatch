#!/usr/bin/env bash
# Fails when the skills inventory, the coverage matrix, or the skill sources drift from the code.
# Checks: every MCP tool, how-to, route, command and setting found by
# scripts/skills-coverage-extract.sh is in docs/skills/inventory.yaml (and the reverse);
# every inventory entry has a matrix row and every row names a known entry;
# every skill named by a row exists in docs/skills/staging/ or docs/skills/registry-skills.txt.
# ROOT redirects every source path; SKILLS_COVERAGE_EXTRACT overrides the extractor.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${ROOT:-$(cd "$HERE/.." && pwd)}"
EXTRACT="${SKILLS_COVERAGE_EXTRACT:-$HERE/skills-coverage-extract.sh}"
INV="$ROOT/docs/skills/inventory.yaml"
MATRIX="$ROOT/docs/skills/coverage-matrix.yaml"
STAGING="$ROOT/docs/skills/staging"
REGISTRY="$ROOT/docs/skills/registry-skills.txt"
export ROOT

fail=0
bad() { echo "FAIL: $*"; fail=1; }
for f in "$INV" "$MATRIX" "$REGISTRY" "$EXTRACT"; do [ -f "$f" ] || { echo "FAIL: missing file: ${f#"$ROOT"/}"; exit 1; }; done

# inv_list <key>: values of one list key across all inventory entries, sorted.
inv_list() {
  awk -v k="$1" '
    /^  [a-z]+:/ { cur = $1; sub(/:$/, "", cur); next }
    /^  - / && cur == k { sub(/^  - /, ""); print }
    /^- / { cur = "" }
  ' "$INV" | LC_ALL=C sort -u
}

drift() { # <surface> <inventory key>
  local want have
  want="$(bash "$EXTRACT" "$1")"; have="$(inv_list "$2")"
  while IFS= read -r x; do [ -n "$x" ] && bad "$1 not in inventory: $x"; done < <(comm -23 <(echo "$want") <(echo "$have"))
  while IFS= read -r x; do [ -n "$x" ] && bad "inventory $2 not found in code or docs: $x"; done < <(comm -13 <(echo "$want") <(echo "$have"))
}
drift mcp-tools tools
drift howtos howtos
drift routes routes
drift commands commands
drift settings settings

ids="$(sed -nE 's/^- id: *([^ ]+).*/\1/p' "$INV" | LC_ALL=C sort -u)"
rows="$(sed -nE 's/^- entry: *([^ ]+).*/\1/p' "$MATRIX" | LC_ALL=C sort -u)"
while IFS= read -r x; do [ -n "$x" ] && bad "inventory entry has no matrix row: $x"; done < <(comm -23 <(echo "$ids") <(echo "$rows"))
while IFS= read -r x; do [ -n "$x" ] && bad "matrix row names unknown entry: $x"; done < <(comm -13 <(echo "$ids") <(echo "$rows"))

known="$( { [ -d "$STAGING" ] && find "$STAGING" -mindepth 1 -maxdepth 1 -type d -printf '%f\n'; grep -vE '^[[:space:]]*(#|$)' "$REGISTRY" || true; } | LC_ALL=C sort -u)"
while IFS= read -r s; do
  [ -n "$s" ] || continue
  grep -qxF "$s" <<<"$known" || bad "matrix skill not in staging or registry list: $s"
done < <(sed -nE 's/^[[:space:]]*skills:[[:space:]]*\[(.*)\].*/\1/p' "$MATRIX" | tr ',' '\n' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')

[ "$fail" -eq 0 ] && echo "ok: skills coverage"
exit "$fail"
