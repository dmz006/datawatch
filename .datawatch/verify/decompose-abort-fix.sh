#!/usr/bin/env bash
. "$(dirname "$0")/lib.sh"
gofmt_named internal/autonomous/decompose.go internal/autonomous/decompose_lint_hook.go internal/autonomous/manager.go internal/autonomous/decompose_output_crash_test.go cmd/datawatch/session_judgment.go
need_file internal/autonomous/decompose_output_crash_test.go
need_file internal/autonomous/testdata/decompose_mergeshadow_output.json
grep -q 'decompose output unparseable' internal/autonomous/decompose.go || fail "named error text missing from decompose.go"
go build ./internal/autonomous/ ./cmd/datawatch/ || fail "go build"
go test ./internal/autonomous/ -run 'DecomposeOutputCrash' -count=1 || fail "decompose output crash tests"
exit $rc
