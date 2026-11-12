#!/usr/bin/env bash
# scripts/check-dependabot-security-only_test.sh — shell tests for
# scripts/check-dependabot-security-only.sh.
#
# Runs the script against the real .github/dependabot.yml (expect success) and
# against small fixture files written under a mktemp -d directory (outside the
# real repository), each expecting failure.
#
# Run independently: bash scripts/check-dependabot-security-only_test.sh
# Exit codes: 0 all cases passed, 1 at least one case failed.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_UNDER_TEST="${REPO_ROOT}/scripts/check-dependabot-security-only.sh"
REAL_FILE="${REPO_ROOT}/.github/dependabot.yml"
[[ -f "$SCRIPT_UNDER_TEST" ]] || { echo "script under test is missing: $SCRIPT_UNDER_TEST" >&2; exit 1; }

if ! bash -n "$SCRIPT_UNDER_TEST"; then
    echo "syntax check failed for $SCRIPT_UNDER_TEST" >&2
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

case_no=0
pass_count=0
fail_count=0

# write_fixture name limit_for_docker [extra top-level lines] -> sets FIXTURE
# limit_for_docker: a number, or "none" to omit the line.
# Ecosystems written: gomod, docker, github-actions (omit one with SKIP_ECO).
write_fixture() {
    local limit="$1" extra="${2:-}"
    case_no=$((case_no + 1))
    FIXTURE="$TMP/fixture-$case_no.yml"
    {
        printf 'version: 2\nupdates:\n'
        for eco in gomod docker github-actions; do
            [[ "${SKIP_ECO:-}" == "$eco" ]] && continue
            printf '  - package-ecosystem: %s\n    directory: "/"\n    schedule:\n      interval: daily\n' "$eco"
            if [[ "$eco" == "docker" ]]; then
                [[ "$limit" == "none" ]] || printf '    open-pull-requests-limit: %s\n' "$limit"
                [[ -z "$extra" ]] || printf '%s\n' "$extra"
            else
                printf '    open-pull-requests-limit: 0\n'
            fi
        done
    } > "$FIXTURE"
}

# expect_case name expected_rc file
expect_case() {
    local name="$1" want_rc="$2" file="$3" out rc
    out="$(bash "$SCRIPT_UNDER_TEST" "$file" 2>&1)"
    rc=$?
    if [[ "$rc" -eq "$want_rc" ]]; then
        pass_count=$((pass_count + 1))
        printf 'ok   %-34s rc=%s\n' "$name" "$rc"
    else
        fail_count=$((fail_count + 1))
        printf 'FAIL %-34s want rc=%s, got rc=%s\n' "$name" "$want_rc" "$rc"
        printf '%s\n' "$out" | sed 's/^/     | /'
    fi
}

# Case 1: the real file passes
expect_case "real dependabot file passes" 0 "$REAL_FILE"

# Case 2: a good fixture passes (guards the fixture writer itself)
write_fixture 0
expect_case "good fixture passes" 0 "$FIXTURE"

# Case 3: a limit of 5 fails
write_fixture 5
expect_case "limit of 5 fails" 1 "$FIXTURE"

# Case 4: a missing limit fails
write_fixture none
expect_case "missing limit fails" 1 "$FIXTURE"

# Case 5: an ignore key fails
write_fixture 0 '    ignore:
      - dependency-name: "foo"'
expect_case "ignore key fails" 1 "$FIXTURE"

# Case 6: a commented ignore key does not fail
write_fixture 0 '    # ignore: not used'
expect_case "commented ignore passes" 0 "$FIXTURE"

# Case 7: a missing ecosystem fails
SKIP_ECO=github-actions write_fixture 0
expect_case "missing ecosystem fails" 1 "$FIXTURE"

# Case 8: a missing file is a usage error
expect_case "missing file is a usage error" 2 "$TMP/does-not-exist.yml"

printf '\n%d case(s) passed, %d case(s) failed\n' "$pass_count" "$fail_count"
if (( fail_count > 0 )); then
    exit 1
fi
exit 0
