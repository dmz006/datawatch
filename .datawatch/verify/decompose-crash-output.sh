#!/usr/bin/env bash
. "$(dirname "$0")/lib.sh"
gofmt_named cmd/datawatch/main.go cmd/datawatch/crash_output.go cmd/datawatch/crash_output_test.go
need_file cmd/datawatch/crash_output.go
need_file cmd/datawatch/crash_output_test.go
[ ! -e cmd/datawatch/dup_unix.go ] || fail "dup_unix.go should be removed"
[ ! -e cmd/datawatch/dup_windows.go ] || fail "dup_windows.go should be removed"
! grep -q 'dupStderrTo' cmd/datawatch/main.go || fail "main.go still calls dupStderrTo"
go build ./cmd/datawatch/ || fail "go build"
GOOS=windows go build -o /dev/null ./cmd/datawatch/ || fail "windows build"
go test ./cmd/datawatch/ -run 'CrashOutput' -count=1 || fail "crash output tests"
exit $rc
