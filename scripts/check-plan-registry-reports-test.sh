#!/usr/bin/env bash
# scripts/check-plan-registry-reports-test.sh — test for
# scripts/check-plan-registry-reports.sh.
#
# Builds a tiny set of fake reconciliation reports in a temporary
# directory (mktemp -d under the scratch area named by TMPDIR — never a
# hard-coded system temp path) and asserts:
#   - the checker passes on the good set;
#     (list count, duplicates, unlisted files are covered by the other
#      assertions and by the real-reports run)
#   - it fails on a duplicated file (a listed file in two batch rows);
#   - it fails on a missing class (an entry whose class cell is empty);
#   - it fails on a class b entry without a proposed row;
#   - it fails on a class c entry without a reason.

set -euo pipefail

unset TMUX TMUX_TMPDIR

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHECKER="$SCRIPT_DIR/check-plan-registry-reports.sh"

SCRATCH_ROOT="${TMPDIR:-$SCRIPT_DIR/../.tmp}"
mkdir -p "$SCRATCH_ROOT"
WORK="$(mktemp -d "$SCRATCH_ROOT/check-plan-registry-reports-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# emit_set <dir> <variant>
# Writes list, two batch reports and a summary covering four files
# (a.md class a; b1.md and b2.md class b with proposed rows; c1.md
# class c with a reason). Variants inject one defect each.
emit_set() {
    local dir="$1" variant="$2"
    mkdir -p "$dir"
    cat > "$dir/plan-registry-reconciliation-list.md" <<'EOF'
count: 4

- a.md
- b1.md
- b2.md
- c1.md
EOF
    local b1_class="b"
    [ "$variant" = "missing-class" ] && b1_class=""
    local c1_reason="investigation only, no plan in the file"
    [ "$variant" = "missing-reason" ] && c1_reason=""
    cat > "$dir/plan-registry-reconciliation-batch-01.md" <<EOF
# Plan registry reconciliation — batch 01 (entries 1 to 2 of 4)

| file | class | evidence | proposed row |
|---|---|---|---|
| a.md | a | already covered by a registry row | |
| b1.md | ${b1_class} | no registry row | Feature one \| done |
EOF
    cat > "$dir/plan-registry-reconciliation-batch-02.md" <<EOF
# Plan registry reconciliation — batch 02 (entries 3 to 4 of 4)

| file | class | evidence | proposed row |
|---|---|---|---|
| b2.md | b | no registry row | Feature two \| done |
| c1.md | c | ${c1_reason} | |
$(case "$variant" in duplicated-file) echo '| a.md | a | duplicated row | |';; esac)
EOF
    cat > "$dir/plan-registry-reconciliation-batch-03.md" <<'EOF'
# batch 03 — no entries

| file | class | evidence | proposed row |
|---|---|---|---|
EOF
    # batches 04 to 06 must exist; empty entry tables are fine here
    # because the checker only aggregates rows it finds
    for i in 04 05 06; do
        cat > "$dir/plan-registry-reconciliation-batch-$i.md" <<'EOF'
# batch — no entries

| file | class | evidence | proposed row |
|---|---|---|---|
EOF
    done
    cat > "$dir/plan-registry-reconciliation-summary.md" <<'EOF'
# Plan registry reconciliation — summary

## Counts per class

- class a: 1
- class b: 2
- class c: 1
- class d: 0
- total: 4 (equal to the list count of 4)
EOF
}

failures=0

run_expect() {
    local label="$1" dir="$2" expect="$3" # expect: pass|fail
    local out rc=0
    out="$("$CHECKER" "$dir" 2>&1)" || rc=$?
    case "$expect:$rc" in
        pass:0)
            echo "ok: $label (checker passed)" ;;
        fail:1|fail:2)
            if grep -q "^FAIL:" <<<"$out"; then
                echo "ok: $label (checker failed as expected: $(grep -m1 '^FAIL:' <<<"$out"))"
            else
                failures=$((failures + 1))
                echo "not ok: $label (non-zero exit but no FAIL message; output: $out)"
            fi ;;
        *)
            failures=$((failures + 1))
            echo "not ok: $label (exit $rc, expected expect)"
            echo "$out" | sed 's/^/    /' ;;
    esac
}

GOOD="$WORK/good";    emit_set "$GOOD" good
DUP="$WORK/dup";      emit_set "$DUP" duplicated-file
NOCLASS="$WORK/noclass"; emit_set "$NOCLASS" missing-class
BEMPTY="$WORK/bempty"
emit_set "$BEMPTY" good
CNO_REASON="$WORK/cnoreason"
emit_set "$CNO_REASON" missing-reason

cat > "$BEMPTY/plan-registry-reconciliation-batch-01.md" <<'EOF'
# Plan registry reconciliation — batch 01 (entries 1 to 2 of 4)

| file | class | evidence | proposed row |
|---|---|---|---|
| a.md | a | already covered by a registry row | |
| b1.md | b | no registry row | |
EOF

run_expect "good set passes"          "$GOOD"    pass
run_expect "duplicated file fails"    "$DUP"     fail
run_expect "missing class fails"      "$NOCLASS" fail
run_expect "class b without row fails" "$BEMPTY" fail
run_expect "class c without reason fails" "$CNO_REASON" fail

if [ "$failures" -ne 0 ]; then
    echo "FAIL: $failures assertion(s) failed" >&2
    exit 1
fi
echo "OK: all plan-registry-reports checker assertions passed"
