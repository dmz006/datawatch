#!/bin/sh
# Fake `go test -json` run used by intermittent_defect_test.go.
#
# It fails on its first run and passes on every later run, like fake-test.sh,
# but prints go test JSON events so the failing test name can be parsed. The
# flag file path comes from MERGESHADOW_FLAG_FILE. It uses no CPU load, no
# sleep and starts no background process.
if [ ! -e "${MERGESHADOW_FLAG_FILE:?MERGESHADOW_FLAG_FILE must be set}" ]; then
	: > "$MERGESHADOW_FLAG_FILE"
	printf '%s\n' '{"Action":"run","Package":"example.com/m/pkg","Test":"TestSharedCounter"}'
	printf '%s\n' '{"Action":"output","Package":"example.com/m/pkg","Test":"TestSharedCounter","Output":"=== RUN   TestSharedCounter\n"}'
	printf '%s\n' '{"Action":"output","Package":"example.com/m/pkg","Test":"TestSharedCounter","Output":"    counter_test.go:41: expected 1 increment, got 2\n"}'
	printf '%s\n' '{"Action":"output","Package":"example.com/m/pkg","Test":"TestSharedCounter","Output":"--- FAIL: TestSharedCounter (0.00s)\n"}'
	printf '%s\n' '{"Action":"fail","Package":"example.com/m/pkg","Test":"TestSharedCounter"}'
	exit 1
fi
echo "ok example.com/m/pkg"
