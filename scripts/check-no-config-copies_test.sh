#!/usr/bin/env bash
# scripts/check-no-config-copies_test.sh — shell tests for
# scripts/check-no-config-copies.sh.
#
# Every case builds a fresh fake git repository under a mktemp -d directory
# (outside the real repository), copies the script under test into the fake
# repository's scripts/ directory, stages a small set of changed/untracked
# files, runs the script, and asserts on the exit status and the reported
# findings count.
#
# Run independently: bash scripts/check-no-config-copies_test.sh
# Exit codes: 0 all cases passed, 1 at least one case failed.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_UNDER_TEST="${REPO_ROOT}/scripts/check-no-config-copies.sh"
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

# ---------------------------------------------------------------------------
# Helpers. All state lives under $TMP; nothing is written to the real repo.
# ---------------------------------------------------------------------------
new_repo() { # sets REPO
    case_no=$((case_no + 1))
    REPO="$TMP/repo-$case_no"
    mkdir -p "$REPO/scripts"
    git -C "$REPO" init -q -b main
    git -C "$REPO" config user.email "test@example.invalid"
    git -C "$REPO" config user.name "Script Test"
    cp "$SCRIPT_UNDER_TEST" "$REPO/scripts/check-no-config-copies.sh"
    printf '# fake repo\n' > "$REPO/README.md"
    (cd "$REPO" && git add README.md && git commit -q -m base)
}

run_check() { # [args...] -> sets RC, OUT, COUNT
    OUT="$(cd "$REPO" && bash scripts/check-no-config-copies.sh "$@" 2>&1)"
    RC=$?
    # findings count: the "config-copy findings: N" line when present, else 0
    COUNT="$(printf '%s\n' "$OUT" | sed -n 's/^config-copy findings: \([0-9][0-9]*\)$/\1/p' | tail -n 1)"
    COUNT="${COUNT:-0}"
}

# expect_case name expected_rc expected_count
# The immediately preceding run_check produced RC/OUT/COUNT.
expect_case() {
    local name="$1" want_rc="$2" want_count="$3"
    if [[ "$RC" -eq "$want_rc" && "$COUNT" -eq "$want_count" ]]; then
        pass_count=$((pass_count + 1))
        printf 'ok   %-34s rc=%s findings=%s\n' "$name" "$RC" "$COUNT"
    else
        fail_count=$((fail_count + 1))
        printf 'FAIL %-34s want rc=%s findings=%s, got rc=%s findings=%s\n' \
            "$name" "$want_rc" "$want_count" "$RC" "$COUNT"
        printf '%s\n' "$OUT" | sed 's/^/     | /'
    fi
}

# ---------------------------------------------------------------------------
# Case 1: no changes at all -> clean
# ---------------------------------------------------------------------------
new_repo
run_check
expect_case "clean tree passes" 0 0

# ---------------------------------------------------------------------------
# Case 2: benign tracked + untracked changes -> clean
# ---------------------------------------------------------------------------
new_repo
printf 'change\n' >> "$REPO/README.md"
mkdir -p "$REPO/internal/foo"
printf 'package foo\n' > "$REPO/internal/foo/bar.go"
printf 'notes\n' > "$REPO/notes.txt"
run_check
expect_case "benign changes pass" 0 0

# ---------------------------------------------------------------------------
# Case 3: allowlisted template and fixture -> clean
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/testdata"
printf 'name: datawatch\n' > "$REPO/testdata/datawatch.yaml"
printf 'DATAWATCH_BUILD_MODE=debug\n' > "$REPO/.env.build.example"
run_check
expect_case "allowlisted template+fixture pass" 0 0

# ---------------------------------------------------------------------------
# Case 4: .env files -> two findings
# ---------------------------------------------------------------------------
new_repo
printf 'TOKEN=abc\n' > "$REPO/.env"
mkdir -p "$REPO/deploy"
printf 'DSN=postgres://x\n' > "$REPO/deploy/prod.env"
run_check
expect_case "env file copies fail" 1 2

# ---------------------------------------------------------------------------
# Case 5: config-copy and backup base names -> two findings
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/deploy/staging"
printf 'name: staging\n' > "$REPO/deploy/staging/config.yaml"
mkdir -p "$REPO/backup"
printf 'stale\n' > "$REPO/backup/notes.bak"
run_check
expect_case "config.yaml + .bak copies fail" 1 2

# ---------------------------------------------------------------------------
# Case 6: copied .datawatch / home config directories -> two findings
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/snapshots/host-a/.datawatch"
printf '{}\n' > "$REPO/snapshots/host-a/.datawatch/state.json"
mkdir -p "$REPO/export/.config/datawatch"
printf 'token: x\n' > "$REPO/export/.config/datawatch/config.json"
run_check
expect_case "copied config dirs fail" 1 2

# ---------------------------------------------------------------------------
# Case 7: secret / key files -> two findings
# ---------------------------------------------------------------------------
new_repo
printf 'PRIVATE KEY MATERIAL\n' > "$REPO/id_ed25519"
mkdir -p "$REPO/tls"
printf 'PRIVATE KEY\n' > "$REPO/tls/server.key"
run_check
expect_case "secret/key files fail" 1 2

# ---------------------------------------------------------------------------
# Case 8: deleting a stray copy in the working tree -> clean (removal is the
# fix and must not be blocked)
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/deploy"
printf 'secret\n' > "$REPO/deploy/prod.env"
(cd "$REPO" && git add deploy/prod.env && git commit -q -m "stray")
rm -f "$REPO/deploy/prod.env"
run_check
expect_case "deleting a stray copy passes" 0 0

# ---------------------------------------------------------------------------
# Case 9: base ref argument flags a committed copy in base..HEAD
# ---------------------------------------------------------------------------
new_repo
git -C "$REPO" branch leakbase
mkdir -p "$REPO/bad"
printf 'TOKEN=abc\n' > "$REPO/bad/.env.local"
(cd "$REPO" && git add bad/.env.local && git commit -q -m "leak")
# Against HEAD the tree is clean...
run_check
expect_case "no base ref: HEAD-relative clean" 0 0
# ...but the range leakbase..HEAD contains the committed copy.
run_check leakbase
expect_case "base ref flags committed copy" 1 1

# ---------------------------------------------------------------------------
# Case 10: usage errors -> exit 2
# ---------------------------------------------------------------------------
new_repo
run_check a b
expect_case "two arguments is a usage error" 2 0
new_repo
run_check no-such-ref
expect_case "unresolvable base ref is a usage error" 2 0

# ---------------------------------------------------------------------------
printf '\n%d case(s) passed, %d case(s) failed\n' "$pass_count" "$fail_count"
if (( fail_count > 0 )); then
    exit 1
fi
exit 0
