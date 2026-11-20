#!/usr/bin/env bash
# Tests for scripts/check-no-pwa-when-spec-forbids.sh.
#
# Builds a throwaway git repo in a mktemp -d directory (removed on EXIT/INT/TERM)
# and checks the four guard behaviours. No fixed calendar dates, no daemons.
set -uo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
guard="$repo_root/scripts/check-no-pwa-when-spec-forbids.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

cd "$tmp"
git init -q -b main
git config user.email guard-test@example.com
git config user.name guard-test
mkdir -p internal/server/web
printf 'console.log("app");\n' > internal/server/web/app.js
printf 'other\n' > other.txt
git add .
git commit -qm 'base'
base=$(git rev-parse HEAD)

fail=0

# Case 1: spec forbids a PWA change and app.js was edited -> exit 1 + exact line.
printf 'Scope: dashboard copy edit. no PWA change.\n' > spec.md
printf 'console.log("changed");\n' > internal/server/web/app.js
if out_err=$(bash "$guard" spec.md "$base" 2>&1 >/dev/null); then
  echo 'case 1 FAIL: expected exit 1, got 0'
  fail=1
elif [ "$out_err" = 'forbidden PWA change: internal/server/web/app.js' ]; then
  echo 'PASS case 1: spec forbids the PWA and app.js changed -> exit 1 with the exact line'
else
  echo "case 1 FAIL: wrong stderr: $out_err"
  fail=1
fi

# Case 2: spec forbids a PWA change, only another file changed -> exit 0.
git reset -q --hard "$base"
printf 'Scope: dashboard copy edit. no PWA change.\n' > spec.md
printf 'changed\n' > other.txt
if bash "$guard" spec.md "$base" >/dev/null 2>&1; then
  echo 'PASS case 2: spec forbids the PWA but app.js untouched -> exit 0'
else
  echo 'case 2 FAIL: expected exit 0, got non-zero'
  fail=1
fi

# Case 3: spec does not mention the PWA and app.js was edited -> exit 0.
git reset -q --hard "$base"
printf 'Scope: adjust the dashboard label copy.\n' > spec.md
printf 'console.log("changed");\n' > internal/server/web/app.js
if bash "$guard" spec.md "$base" >/dev/null 2>&1; then
  echo 'PASS case 3: spec does not forbid the PWA -> exit 0 even with app.js changed'
else
  echo 'case 3 FAIL: expected exit 0, got non-zero'
  fail=1
fi

# Case 4: missing spec file -> exit 2.
git reset -q --hard "$base"
rm -f spec.md
if bash "$guard" spec.md "$base" >/dev/null 2>&1; then
  echo 'case 4 FAIL: expected exit 2, got 0'
  fail=1
else
  rc=$?
  if [ "$rc" -eq 2 ]; then
    echo 'PASS case 4: missing spec file -> exit 2'
  else
    echo "case 4 FAIL: expected exit 2, got $rc"
    fail=1
  fi
fi

exit "$fail"
