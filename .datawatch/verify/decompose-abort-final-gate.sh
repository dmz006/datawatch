#!/usr/bin/env bash
. "$(dirname "$0")/lib.sh"
for s in decompose-abort-fix decompose-crash-output decompose-abort-docs; do
  bash "$(dirname "$0")/$s.sh" || fail "$s"
done
go vet ./internal/autonomous/ ./cmd/datawatch/ || fail "go vet"
go test ./internal/autonomous/ -run 'Decompose' -count=1 || fail "autonomous decompose tests"
exit $rc
