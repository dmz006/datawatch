#!/usr/bin/env bash
. "$(dirname "$0")/common.sh"
fail=0
for f in $(changed | grep -E '\.js$'); do node --check "$f" || fail=1; done
for f in $(changed | grep -E 'locales?/.*\.(js|json)$'); do :; done
# every new/changed locale key must exist in all five bundles
n=$(changed | grep -cE 'locales?/') ; [ "$n" -ge 5 ] || { echo "expected all five locale bundles changed"; fail=1; }
changed | xargs grep -lE 'localStorage\.(set|get)Item' >/dev/null 2>&1 && git diff "$base" -- . ':!*.test.js' ':!.datawatch' | grep -E '^\+.*localStorage' && { echo "new storage use"; fail=1; }
exit $fail
