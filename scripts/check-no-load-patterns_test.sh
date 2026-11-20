#!/usr/bin/env bash
# scripts/check-no-load-patterns_test.sh — proves scripts/check-no-load-patterns.sh
# passes on a clean tree and fails on a tree seeded with each pattern.
# Fixture trees are built under a mktemp -d folder and passed through the
# script's root argument. Fixtures are text only: nothing here runs a burner,
# spawns load, or starts a background process.
# Run: bash scripts/check-no-load-patterns_test.sh   (exit 0 all pass, 1 any fail)

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SUT="$REPO_ROOT/scripts/check-no-load-patterns.sh"
[[ -f "$SUT" ]] || { echo "script under test missing: $SUT" >&2; exit 1; }
bash -n "$SUT" || { echo "syntax check failed" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
n=0; fails=0

new_tree() { n=$((n + 1)); T="$TMP/t$n"; mkdir -p "$T/internal/x" "$T/scripts/tests" "$T/tests" "$T/internal/server/web"; }

expect() { # name want_rc [grep-pattern-in-output]
    local name="$1" want="$2" pat="${3:-}" out rc
    out="$(bash "$SUT" "$T" 2>&1)"; rc=$?
    if [[ $rc -ne $want ]] || { [[ -n "$pat" ]] && ! grep -qF -- "$pat" <<<"$out"; }; then
        echo "FAIL: $name (rc=$rc want=$want)"; echo "$out" | sed 's/^/    /'; fails=$((fails + 1))
    else
        echo "ok:   $name"
    fi
}

seed() { # name relpath line
    new_tree
    mkdir -p "$(dirname "$T/$2")"
    printf '%s\n' "package x" "$3" > "$T/$2"
    expect "$1" 1 "$2:2:"
}

new_tree
cat > "$T/internal/x/a_test.go" <<'G'
package x
func TestA(t *testing.T) { for i := 0; i < 3; i++ { _ = i } }
G
printf '#!/bin/sh\nwhile :; do sleep 1; done\n' > "$T/scripts/tests/ok.sh"
printf 'assert(msg.includes("stress"))\n' > "$T/internal/server/web/a.test.js"
expect "clean tree passes" 0 "OK"

seed "while : busy loop"        scripts/tests/a.sh      'while :; do :; done'
seed "while true busy loop"     tests/b.sh              'while true; do :; done'
seed "yes to /dev/null"         tests/c.sh              'yes > /dev/null'
seed "stress command"           tests/d.sh              'stress --cpu 4'
seed "stress-ng command"        tests/e.sh              'timeout 5 stress-ng --cpu 2'
seed "cpuburn command"          tests/f.sh              'cpuburn &'
seed "dd zero to null"          scripts/tests/g.sh      'dd if=/dev/zero of=/dev/null'
seed "Go for {}"                internal/x/h_test.go    'func f() { for {} }'
seed "Go for { }"               internal/x/i_test.go    'func f() { for { } }'
seed "GOMAXPROCS load fan-out"  internal/x/j_test.go    '// load: n := runtime.GOMAXPROCS(0) workers'
seed "startConcurrentLoad"      internal/x/k_test.go    'func startConcurrentLoad() {}'
seed "loadWorkers"              internal/x/l_test.go    'var loadWorkers = 4'
seed "exec.Command stress"      internal/x/m_test.go    'c := exec.Command("stress", "--cpu", "2")'
seed "testdata file"            internal/x/testdata/n.sh 'yes > /dev/null'
seed "JS test file"             internal/server/web/o.test.js 'while :; do :; done'

# Allow-list suppresses a listed path only.
new_tree
printf 'package x\nvar loadWorkers = 1\n' > "$T/internal/x/p_test.go"
printf 'internal/x/p_test.go\n' > "$T/scripts/check-no-load-patterns.allow"
expect "allow-list path is skipped" 0 "OK"

# Non-test files are out of scope.
new_tree
printf 'package x\nvar loadWorkers = 1\n' > "$T/internal/x/prod.go"
expect "non-test file ignored" 0 "OK"

# Several hits are all listed.
new_tree
printf 'package x\nvar loadWorkers = 1\nfunc startConcurrentLoad() {}\n' > "$T/internal/x/q_test.go"
expect "multiple hits listed" 1 "q_test.go:3:"

# Bad root.
out="$(bash "$SUT" "$TMP/nope" 2>&1)"; rc=$?
if [[ $rc -eq 2 ]]; then echo "ok:   bad root exits 2"; else echo "FAIL: bad root (rc=$rc)"; fails=$((fails + 1)); fi

[[ $fails -eq 0 ]] && { echo "all passed"; exit 0; }
echo "$fails failed"; exit 1
