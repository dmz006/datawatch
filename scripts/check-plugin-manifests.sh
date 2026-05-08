#!/usr/bin/env bash
# scripts/check-plugin-manifests.sh — enforces the "Plugin-Manifest
# Validation" rule (AGENT.md, BL274 closure).
#
# Per BL274 Q9: a plugin manifest may declare a `docs:` block. When
# present, `docs.files:` is REQUIRED (a list of operator-readable doc
# paths relative to the plugin dir) and every listed file must exist.
# `docs.howtos:` is optional metadata; if present, every entry's `file`
# must also exist.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

# Validate every manifest.yaml under examples/plugins/ + ~/.datawatch/plugins/.
declare -a ROOTS
[[ -d examples/plugins ]] && ROOTS+=("examples/plugins")
[[ -d "$HOME/.datawatch/plugins" ]] && ROOTS+=("$HOME/.datawatch/plugins")

if [[ ${#ROOTS[@]} -eq 0 ]]; then
    echo "==> Plugin manifest audit (no plugin roots found — skipping)"
    exit 0
fi

echo "==> Plugin manifest audit (roots: ${ROOTS[*]})"

while IFS= read -r manifest; do
    plugin_dir="$(dirname "$manifest")"
    # Skip non-yaml manifests + nested vendor dirs.
    [[ -f "$manifest" ]] || continue
    # Only act if the manifest declares docs: at top level.
    if ! grep -q '^docs:' "$manifest"; then
        continue
    fi
    # Pull every `docs.files:` and `docs.howtos[].file:` value.
    mapfile -t FILES < <(awk '
        /^docs:/{indocs=1; next}
        indocs && /^[a-z]/ && !/^docs:/ {indocs=0}
        indocs && /^[[:space:]]+files:/{infiles=1; next}
        indocs && /^[[:space:]]+[a-z]/{infiles=0}
        infiles && /^[[:space:]]+- /{
            sub(/^[[:space:]]+-[[:space:]]*/, "")
            gsub(/"/, "")
            print
        }
        indocs && /^[[:space:]]+- file:/{
            sub(/^[[:space:]]+-[[:space:]]*file:[[:space:]]*/, "")
            gsub(/"/, "")
            print
        }
    ' "$manifest")
    if [[ ${#FILES[@]} -eq 0 ]]; then
        report "$manifest declares docs: but no files: list (Q9 requires files)"
        continue
    fi
    for f in "${FILES[@]}"; do
        full="$plugin_dir/$f"
        if [[ ! -f "$full" ]]; then
            report "$manifest references non-existent doc: $f (looked in $full)"
        fi
    done
done < <(find "${ROOTS[@]}" -name 'manifest.yaml' 2>/dev/null)

if [[ $ERRORS -gt 0 ]]; then
    echo
    echo "✗ FAIL: $ERRORS plugin manifest issue(s)."
    echo "  Rule: AGENT.md §Plugin-Manifest Validation (BL274 Q9) — every"
    echo "  plugin manifest's docs:files: must list files that exist."
    exit 1
fi

echo "✓ PASS: every plugin manifest's docs:files: + howtos[].file references existing files."
exit 0
