#!/usr/bin/env bash
. "$(dirname "$0")/lib.sh"
make sync-docs >/dev/null 2>&1 || fail "make sync-docs"
sh scripts/check-docs-sync.sh >/dev/null 2>&1 || fail "check-docs-sync"
for t in decompose-parse daemon-crash.log coredumpctl; do
  grep -q -- "$t" docs/howto/decompose-async.md docs/operations.md || fail "docs do not mention $t"
done
grep -q 'coredumpctl' docs/operations.md || fail "operations.md lacks coredumpctl"
grep -q 'decompose-parse' docs/howto/decompose-async.md || fail "decompose-async.md lacks decompose-parse"
exit $rc
