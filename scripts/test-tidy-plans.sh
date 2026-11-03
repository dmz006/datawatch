#!/usr/bin/env bash
#
# test-tidy-plans.sh — self-contained tests for scripts/tidy-plans.sh,
# scripts/plans_links.py, and scripts/check-plans-links.sh.
#
# Builds a throwaway git repo under a temp dir and exercises the full
# contract: kept vs archived classification by references, relative and
# absolute link rewriting, registry handling, release-notes grouping,
# count-only output (never a file list), --dry-run, --check, idempotency,
# and the dangling-link checker.  The real docs/plans/ is never touched.

set -euo pipefail

SELF_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_DIR=$(cd "$SELF_DIR/.." && pwd)

FAILED=0
ok()   { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1" >&2; FAILED=1; }

assert_contains() { # $1=what  $2=haystack  $3=literal substring
  case "$2" in
    *"$3"*) ok "$1" ;;
    *) printf '     want substring: %s\n     got:\n%s\n' "$3" "$2" >&2; fail "$1" ;;
  esac
}
assert_line() { # $1=what  $2=haystack  $3=literal line prefix
  printf '%s\n' "$2" | grep -Eq "^$3" || { printf '     got:\n%s\n' "$2" >&2; fail "$1"; return; }
  ok "$1"
}
assert_not_line() {
  if printf '%s\n' "$2" | grep -Eq "^$3"; then printf '     found: %s\n' "$3" >&2; fail "$1"; else ok "$1"; fi
}
assert_no_file_paths() { # count-only output: no line may reference a .md file
  if printf '%s\n' "$2" | grep -qE '\.md\b'; then printf '     got:\n%s\n' "$2" >&2; fail "$1"; else ok "$1"; fi
}
assert_file()  { [ -f "$2" ] && ok "$1" || fail "$1 (missing $2)"; }
assert_no_file() { [ ! -e "$2" ] && ok "$1" || fail "$1 (unexpected $2)"; }

# ── fixture repo ─────────────────────────────────────────────────────────
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
git init -q .
git config user.name "tidy-test"
git config user.email "tidy-test@localhost"

mkdir -p docs/plans docs scripts cmd/datawatch
# the code under test: pristine copies from the real repo
cp "$REPO_DIR/scripts/plans_links.py"       scripts/
cp "$REPO_DIR/scripts/tidy-plans.sh"        scripts/
cp "$REPO_DIR/scripts/check-plans-links.py" scripts/

printf 'package main\n\nvar Version = "1.2.3"\n' > cmd/datawatch/main.go

cat > docs/plans/README.md <<'EOF'
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [done plan](2020-01-01-done-plan.md) | done, shipped in 1.2.0 |
| 2 | [open plan](2020-01-02-open-plan.md) | not started |
| 3 | [row ref plan](2020-01-03-row-ref.md) | done |
| 4 | [abs ref plan](docs/plans/2020-01-04-abs-ref.md) | done, merged |
EOF

for p in 2020-01-01-done-plan 2020-01-02-open-plan 2020-01-03-row-ref \
         2020-01-04-abs-ref 2020-01-05-docs-ref 2020-01-06-script-ref \
         2020-01-07-rule-ref; do
  printf '# %s\n\nContent.\n' "$p" > "docs/plans/$p.md"
done
# fresh plan: today's date, no references -> kept by age
printf '# recent\n' > "docs/plans/$(date +%Y-%m-%d)-recent-plan.md"

# referencing surfaces (each keeps its plan)
cat > docs/other-guide.md <<'EOF'
See [docs ref](plans/2020-01-05-docs-ref.md) for details.
EOF
cat > scripts/tool.sh <<'EOF'
#!/usr/bin/env bash
# reference: docs/plans/2020-01-06-script-ref.md
echo ok
EOF
cat > AGENT.md <<'EOF'
# rules
- see [rule ref](docs/plans/2020-01-07-rule-ref.md)
EOF

# release notes: one stale minor, one current; one unclassified file
printf 'old notes\n'  > docs/plans/RELEASE-NOTES-v0.9.1.md
printf 'cur notes\n'  > docs/plans/RELEASE-NOTES-v1.2.0.md
printf 'scratch\n'    > docs/plans/notes.md

git add -A
git commit -qm init
cd "$TMP"

# ── 1. dry run: exact counts, nothing moved, no file paths ──────────────
out=$(bash scripts/tidy-plans.sh --dry-run)
assert_line  "dry-run: plans kept"           "$out" 'plans kept: 5'
assert_line  "dry-run: plans archived"       "$out" 'plans archived: 3'
assert_line  "dry-run: links rewritten"      "$out" 'links rewritten: 3'
assert_line  "dry-run: release notes moved"  "$out" 'release notes moved: 1'
assert_line  "dry-run: unclassified counted" "$out" 'unclassified \(manual review needed\): 1'
assert_line  "dry-run: marker printed"       "$out" '^\(dry run'
assert_no_file_paths "dry-run: count-only output (no .md lines)" "$out"
assert_file "dry-run: archived plan still in place"      "docs/plans/2020-01-01-done-plan.md"
assert_no_file "dry-run: historical-plans/ not created"  "docs/plans/historical-plans"
assert_file "dry-run: stale release note still in place" "docs/plans/RELEASE-NOTES-v0.9.1.md"

# ── 2. --check fails while anything would move ──────────────────────────
if bash scripts/tidy-plans.sh --check >/dev/null 2>&1; then
  fail "--check: expected non-zero exit while plans are stale"
else
  ok "--check: non-zero exit while plans are stale"
fi

# ── 3. real run: moves, link rewrites, release notes ────────────────────
out=$(bash scripts/tidy-plans.sh)
assert_line  "run: plans kept"           "$out" 'plans kept: 5'
assert_line  "run: plans archived"       "$out" 'plans archived: 3'
assert_line  "run: links rewritten"      "$out" 'links rewritten: 3'
assert_line  "run: release notes moved"  "$out" 'release notes moved: 1'
assert_no_file_paths "run: count-only output (no .md lines)" "$out"

assert_file "run: bare-row plan moved"    "docs/plans/historical-plans/2020-01-01-done-plan.md"
assert_file "run: row-ref plan moved"     "docs/plans/historical-plans/2020-01-03-row-ref.md"
assert_file "run: abs-ref plan moved"     "docs/plans/historical-plans/2020-01-04-abs-ref.md"
assert_no_file "run: open-row plan kept"  "docs/plans/historical-plans/2020-01-02-open-plan.md"
assert_file  "run: open-row plan still top-level"  "docs/plans/2020-01-02-open-plan.md"
assert_file  "run: docs-referenced plan kept"     "docs/plans/2020-01-05-docs-ref.md"
assert_file  "run: scripts-referenced plan kept"  "docs/plans/2020-01-06-script-ref.md"
assert_file  "run: rules-referenced plan kept"    "docs/plans/2020-01-07-rule-ref.md"
assert_file  "run: fresh plan kept"               "docs/plans/$(date +%Y-%m-%d)-recent-plan.md"
assert_file  "run: stale release note moved"      "docs/plans/historical-releasenotes/0.9/RELEASE-NOTES-v0.9.1.md"
assert_file  "run: current release note kept"     "docs/plans/RELEASE-NOTES-v1.2.0.md"
assert_file  "run: unclassified file kept"        "docs/plans/notes.md"

reg=$(cat docs/plans/README.md)
assert_contains "run: bare registry link rewritten"  "$reg" '](historical-plans/2020-01-03-row-ref.md)'
assert_contains "run: closed-row bare link rewritten" "$reg" '](historical-plans/2020-01-01-done-plan.md)'
assert_contains "run: absolute registry link rewritten" "$reg" '](docs/plans/historical-plans/2020-01-04-abs-ref.md)'
assert_contains "run: open-row link untouched"   "$reg" '](2020-01-02-open-plan.md)'
# no un-rewritten bare registry links to moved plans remain
if grep -qE '\]\(2020-01-0[134]-' docs/plans/README.md; then
  fail "run: un-rewritten registry link remains"
else
  ok "run: no un-rewritten registry links remain"
fi

# ── 4. idempotency: second run is a no-op ───────────────────────────────
out=$(bash scripts/tidy-plans.sh)
assert_line "rerun: nothing archived"     "$out" 'plans archived: 0'
assert_line "rerun: nothing rewritten"    "$out" 'links rewritten: 0'
assert_line "rerun: all plans kept"       "$out" 'plans kept: 5'
assert_line "rerun: no release notes"     "$out" 'release notes moved: 0'

# ── 5. --check passes once the tree is tidy ─────────────────────────────
if bash scripts/tidy-plans.sh --check >/dev/null 2>&1; then
  ok "--check: clean exit after tidy"
else
  fail "--check: expected clean exit after tidy"
fi

# ── 6. dangling-link checker: passes, then catches an injected bad link ─
out=$(python3 scripts/check-plans-links.py "$TMP")
assert_line "checker: zero dangling after tidy" "$out" 'dangling plan references: 0'
if bash -n "$REPO_DIR/scripts/check-plans-links.sh"; then ok "checker: wrapper syntax"; else fail "checker: wrapper syntax"; fi

echo 'See [ghost](plans/2019-05-05-ghost-plan.md).' > docs/bad-guide.md
if python3 scripts/check-plans-links.py "$TMP" >/dev/null 2>&1; then
  fail "checker: expected non-zero exit with a dangling link"
else
  ok "checker: non-zero exit with a dangling link"
fi
out=$(python3 scripts/check-plans-links.py "$TMP" || true)
assert_line "checker: counts the dangling link" "$out" 'dangling plan references: 1'
rm docs/bad-guide.md

# ── 7. python module direct: py_compile + standalone counts ─────────────
if python3 -I -m py_compile scripts/plans_links.py scripts/check-plans-links.py; then
  ok "python: py_compile"
else
  fail "python: py_compile"
fi
out=$(python3 scripts/plans_links.py "$TMP" --dry-run)
assert_line "python: standalone kept"      "$out" 'plans kept: 5'
assert_line "python: standalone archived"  "$out" 'plans archived: 0'
assert_no_file_paths "python: count-only output" "$out"

# ── 8. days threshold: huge window keeps everything ─────────────────────
out=$(python3 "$TMP/scripts/plans_links.py" "$TMP" --days 36500 --dry-run)
assert_line "days: huge threshold keeps all" "$out" 'plans archived: 0'

echo
if [ "$FAILED" -eq 0 ]; then
  echo "tidy-plans tests: all passed"
else
  echo "tidy-plans tests: FAILURES (see above)" >&2
  exit 1
fi
