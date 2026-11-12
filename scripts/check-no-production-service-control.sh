#!/usr/bin/env bash
# scripts/check-no-production-service-control.sh — blocks daemon-reachable code
# from stopping / restarting / killing the production datawatch service:
#
#   family 1: service-manager control of the datawatch unit
#             (systemctl/smctl/launchctl ... stop|restart|reload|kill|off ... datawatch)
#   family 2: the datawatch CLI stop/restart commands
#             (datawatch stop, datawatch restart)
#   family 3: kill-by-name (pkill/killall ... datawatch)
#
# Read-only service inspection (systemctl status / is-active / show-environment)
# is not a violation, and neither is restarting a daemon the script itself
# started (that must still verify the pid belongs to that process — see the
# findings doc, "reachable by daemon" section).
#
# A line whose matching text is fixture data only (never executed against a
# service manager, e.g. expected-string test data) may carry a
# `service-scope-ok` marker comment and is skipped. Comment-only lines are
# skipped. Read-only verification scripts that reference stop-cause behavior
# in prose carry the marker where their text matches.
#
# Scans scripts/, tests/ and .datawatch/verify/ (when present) plus every
# *_test.go in the tree (vendor/ and .git/ excluded). The checker itself never
# runs a service command — it only greps. CHECK_ROOT overrides the scanned
# root (tests point it at a fixture tree).
# Run independently: bash scripts/check-no-production-service-control.sh

set -euo pipefail

ROOT="${CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Production service-control audit (stop/restart/kill of the datawatch unit)"

SELF="check-no-production-service-control.sh"

F1='(systemctl|smctl|launchctl|systemd)[^|;&]*[[:space:]](stop|restart|reload|kill|off)[^|;&]*[[:space:]]datawatch'
F2='(^|[^[:alnum:]_.-])datawatch[[:space:]]+(stop|restart)([^[:alnum:]_.-]|$)'
F3='(pkill|killall)[^&#|;]*[[:space:]]datawatch' # no '#' crossing: a pkill of another app with a datawatch mention in a trailing comment must not match # daemon-cleanup-ok: the audit's own regex for a banned word
FAM="$F1|$F2|$F3"

targets=()
for d in scripts tests .datawatch/verify; do
    [[ -d "$d" ]] && targets+=("$d")
done
while IFS= read -r g; do
    targets+=("$g")
done < <(find . -name '*_test.go' -not -path './vendor/*' -not -path './.git/*' -print 2>/dev/null | sort)

if ((${#targets[@]} > 0)); then
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        file="${hit%%:*}"
        rest="${hit#*:}"
        num="${rest%%:*}"
        line="${rest#*:}"
        [[ "$(basename "$file")" == "$SELF" ]] && continue
        # comment-only lines are exempt (shell: '#', go: '//')
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ "$line" =~ ^[[:space:]]*// ]] && continue
        # fixture data never executed against a service manager
        [[ "$line" == *service-scope-ok* ]] && continue
        report "$file:$num: $(echo "$line" | sed -E 's/^[[:space:]]+//')"
    done < <(grep -rnIE "$FAM" "${targets[@]}" 2>/dev/null | sort -u || true)
fi

if ((ERRORS > 0)); then
    echo
    echo "  hint: daemon-reachable code must not stop/restart/kill the production"
    echo "  datawatch unit, run the datawatch CLI stop/restart commands, or kill"
    echo "  datawatch by name. If the text is fixture data that is never executed"
    echo "  against a service manager, add a 'service-scope-ok' marker comment."
    echo "  Live restart tests must restart only a sandbox daemon pid they verified"
    echo "  (DW_LIVE_RESTART_CMD + DW_LIVE_SANDBOX_PID), never a default unit."
    echo "FAIL: $ERRORS production service-control hit(s)"
    exit 1
fi
echo "OK: no production service-control hits"
