#!/usr/bin/env bash
# scripts/check-no-unscoped-tmux.sh — blocks test/verify scripts from running
# `tmux kill-server` or `tmux kill-session -a` against the operator's live
# tmux server (the TMUX_TMPDIR kill-server incident).
#
# A line that kills a server / all sessions passes only if it uses one of:
#   1. an explicit throwaway socket:  tmux -S "$(mktemp -d)/s" ...  (mktemp,
#      $TMPDIR, /tmp, or a $S/$SOCK/$sock variable set from mktemp)
#   2. a scrubbed environment:        env -u TMUX tmux ...
#      (lines that mention TMUX_TMPDIR must also pass -u TMUX_TMPDIR)
# `TMUX_TMPDIR=... tmux kill-server` alone is a violation.
# A line whose tmux text is only fixture data (never executed against tmux)
# may carry a `tmux-scope-ok` marker comment to be skipped.
#
# Scans scripts/, tests/ and .datawatch/verify/. The checker never runs tmux.
# CHECK_ROOT overrides the root (tests point it at a fixture tree).
# Run independently: env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-unscoped-tmux.sh

set -euo pipefail

ROOT="${CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Unscoped tmux kill audit (kill-server / kill-session -a)"

SELF="check-no-unscoped-tmux.sh"
KILL_RE='tmux([[:space:]]+[^|;&]*)?[[:space:]]+(kill-server|kill-session([[:space:]]+[^|;&]*)?[[:space:]]-[A-Za-z]*a)'
SOCK_RE='-S[[:space:]]*["'"'"']?[^[:space:]]*(mktemp|TMPDIR|/tmp/|\$S\b|\$\{S\}|\$SOCK\b|\$\{SOCK\}|\$sock\b|\$\{sock\})'

dirs=()
for d in scripts tests .datawatch/verify; do
    [[ -d "$d" ]] && dirs+=("$d")
done

if ((${#dirs[@]} > 0)); then
    while IFS= read -r hit; do
        file="${hit%%:*}"
        rest="${hit#*:}"
        num="${rest%%:*}"
        line="${rest#*:}"
        [[ "$(basename "$file")" == "$SELF" ]] && continue
        # comment-only lines are exempt
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ "$line" == *tmux-scope-ok* ]] && continue
        ok=0
        if [[ "$line" =~ $SOCK_RE ]]; then
            ok=1
        elif [[ "$line" =~ env[[:space:]]+(-[A-Za-z]+[[:space:]]+)*-u[[:space:]]+TMUX([[:space:]]|$) ]]; then
            ok=1
            if [[ "$line" == *TMUX_TMPDIR* ]] && ! [[ "$line" =~ -u[[:space:]]+TMUX_TMPDIR ]]; then
                ok=0
            fi
        fi
        if ((ok == 0)); then
            report "$file:$num: unscoped tmux kill: $(echo "$line" | sed -E 's/^[[:space:]]+//')"
        fi
    done < <(grep -rnIE "$KILL_RE" "${dirs[@]}" 2>/dev/null || true)
fi

if ((ERRORS > 0)); then
    echo
    echo "  hint: scope every kill-server / kill-session -a to a throwaway tmux:"
    echo "    1. tmux -S \"\$(mktemp -d)/sock\" kill-server      (explicit throwaway socket)"
    echo "    2. env -u TMUX -u TMUX_TMPDIR tmux kill-server    (scrubbed environment)"
    echo "  TMUX_TMPDIR=... tmux kill-server alone hits the live server."
    echo "FAIL: $ERRORS unscoped tmux kill(s)"
    exit 1
fi
echo "OK: no unscoped tmux kills"
