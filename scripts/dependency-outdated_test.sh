#!/usr/bin/env bash
# scripts/dependency-outdated_test.sh — runs only '--list-pins' (no network) against the real repo.
# Run: bash scripts/dependency-outdated_test.sh
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"
S=scripts/dependency-outdated.sh
fails=0
ok()   { echo "PASS: $1"; }
bad()  { echo "FAIL: $1" >&2; fails=$((fails+1)); }

bash -n "$S" || { echo "syntax error" >&2; exit 1; }
before="$(git status --porcelain)"
out="$(mktemp)"; err="$(mktemp)"
trap 'rm -f "$out" "$err"' EXIT
bash "$S" --list-pins >"$out" 2>"$err"; rc=$?

# A pin without a version comment is an error line on stderr plus a non-zero exit — never anything else.
if [[ $rc -eq 0 ]]; then ok "exit 0"
elif [[ $rc -eq 1 ]] && ! grep -qv '^error: ' "$err" && [[ -s "$err" ]]; then ok "non-zero exit only with error lines on stderr"
else bad "unexpected exit $rc / stderr: $(cat "$err")"; fi

jq -e . "$out" >/dev/null 2>&1 && ok "every line is JSON" || bad "non-JSON output"
for eco in gomod docker github-actions; do
    n="$(jq -c "select(.ecosystem==\"$eco\")" "$out" | wc -l)"
    [[ $n -gt 0 ]] && ok "$eco present ($n)" || bad "$eco missing"
done
badact="$(jq -c 'select(.ecosystem=="github-actions") | select((.hash|test("^[0-9a-f]{40}$")|not) or ((.version//"")==""))' "$out")"
[[ -z "$badact" ]] && ok "actions have 40-char hash and version" || bad "bad action lines: $badact"
jq -e 'select(.ecosystem=="docker" and .unresolved_arg==true)' "$out" >/dev/null 2>&1 \
    && ok "unresolved_arg reported" || bad "no unresolved_arg line"
[[ "$(git status --porcelain)" == "$before" ]] && ok "read-only (git status unchanged)" || bad "git status changed"

[[ $fails -eq 0 ]] && echo "all passed" || { echo "$fails failed" >&2; exit 1; }
