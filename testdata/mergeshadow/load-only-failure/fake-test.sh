#!/bin/sh
# Fake test used by replay_load_only_failure_test.go.
#
# It passes when run alone. It fails only when the environment variable
# MERGESHADOW_CONCURRENT_RUN is set, which stands in for a run that shares
# the machine with other load and loses a timing race.
if [ -n "${MERGESHADOW_CONCURRENT_RUN:-}" ]; then
	echo "race: worker reached the shared counter before its lock was taken"
	echo "expected 1 increment, got 2"
	echo "FAIL fake-test (only under concurrency)"
	exit 1
fi
echo "ok fake-test"
