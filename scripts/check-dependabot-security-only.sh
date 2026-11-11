#!/usr/bin/env bash
# scripts/check-dependabot-security-only.sh — verify that the Dependabot file
# keeps version-update pull requests off, so Dependabot stays security-only.
#
# Usage: scripts/check-dependabot-security-only.sh [path]
#        (path defaults to .github/dependabot.yml)
#
# Fails (exit 1) unless:
#   - every 'package-ecosystem' entry has 'open-pull-requests-limit: 0';
#   - the gomod, docker and github-actions ecosystems are all present;
#   - the file has no uncommented 'ignore:' key (an ignore could hide a
#     security update).
# Exit 2 on a usage error (unreadable file, too many arguments).

set -euo pipefail

if (( $# > 1 )); then
    echo "usage: $0 [path]" >&2
    exit 2
fi
FILE="${1:-.github/dependabot.yml}"
if [[ ! -f "$FILE" ]]; then
    echo "check-dependabot-security-only: file not found: $FILE" >&2
    exit 2
fi

fail=0
problem() { echo "check-dependabot-security-only: $*" >&2; fail=1; }

# Walk the file once. Comment-only lines are skipped and trailing comments are
# stripped. Prints one "eco<TAB>limit" line per package-ecosystem entry (limit
# is "-" when the entry has none) and "IGNORE<TAB>line" for an ignore key.
parsed="$(awk '
    function flush() { if (eco != "") printf "%s\t%s\n", eco, (lim == "" ? "-" : lim); eco = ""; lim = "" }
    {
        line = $0
        sub(/[[:space:]]+#.*$/, "", line)
        if (line ~ /^[[:space:]]*#/) next
        if (line ~ /^[[:space:]]*(-[[:space:]]+)?package-ecosystem:/) {
            flush()
            v = line; sub(/^[^:]*:[[:space:]]*/, "", v)
            gsub(/["\047[:space:]]/, "", v)
            eco = v
        } else if (line ~ /^[[:space:]]*open-pull-requests-limit:/) {
            v = line; sub(/^[^:]*:[[:space:]]*/, "", v)
            gsub(/["\047[:space:]]/, "", v)
            lim = v
        } else if (line ~ /^[[:space:]]*(-[[:space:]]+)?ignore:/) {
            printf "IGNORE\t%d\n", NR
        }
    }
    END { flush() }
' "$FILE")"

for want in gomod docker github-actions; do
    if ! printf '%s\n' "$parsed" | awk -F'\t' -v w="$want" '$1 == w { f = 1 } END { exit !f }'; then
        problem "$FILE: missing package-ecosystem '$want'"
    fi
done

while IFS=$'\t' read -r eco lim; do
    [[ -z "$eco" ]] && continue
    if [[ "$eco" == "IGNORE" ]]; then
        problem "$FILE:$lim: uncommented 'ignore:' key (could hide a security update)"
    elif [[ "$lim" == "-" ]]; then
        problem "$FILE: ecosystem '$eco' has no open-pull-requests-limit (must be 0)"
    elif [[ "$lim" != "0" ]]; then
        problem "$FILE: ecosystem '$eco' has open-pull-requests-limit: $lim (must be 0)"
    fi
done <<< "$parsed"

if (( fail )); then
    exit 1
fi

echo "dependabot is security-only: every ecosystem has open-pull-requests-limit: 0"
echo "reminder: 'Dependabot security updates' is a hosting setting, not in this file; it is not checked here."
