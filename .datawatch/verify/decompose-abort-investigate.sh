#!/usr/bin/env bash
. "$(dirname "$0")/lib.sh"
r=.datawatch/reports/decompose-abort-cause.md
need_file "$r"
grep -q '^Cause:' "$r" || fail "report has no line starting with Cause:"
grep -q '^Evidence:' "$r" || fail "report has no line starting with Evidence:"
exit $rc
