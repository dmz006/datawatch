#!/bin/sh
# Fake test used by replay_intermittent_failure_test.go.
#
# It fails on its first run and passes on every later run. The flag file path
# comes from the environment variable MERGESHADOW_FLAG_FILE. First run: the
# flag file is absent, so the script creates it and exits 1. Later runs: the
# flag file exists, so the script prints "ok fake-test" and exits 0. It uses no
# CPU load and starts no background process.
if [ ! -e "${MERGESHADOW_FLAG_FILE:?MERGESHADOW_FLAG_FILE must be set}" ]; then
	: > "$MERGESHADOW_FLAG_FILE"
	echo "race: worker reached the shared counter before its lock was taken"
	echo "expected 1 increment, got 2"
	echo "FAIL fake-test (first run)"
	exit 1
fi
echo "ok fake-test"
