//go:build !windows

package main

import (
	"os"
	"testing"
)

// TestRunningUnderSystemd is a regression test for an incident found live
// 2026-10-05: a PWA restart left the production daemon down for ~15
// minutes because daemonRestartFn unconditionally spawned selfRestart's
// detached respawn chain (orphaned outside systemd's tracking) instead of
// letting Restart=on-failure bring it back. daemonRestartFn now branches on
// this check first.
func TestRunningUnderSystemd(t *testing.T) {
	orig, had := os.LookupEnv("INVOCATION_ID")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("INVOCATION_ID", orig)
		} else {
			_ = os.Unsetenv("INVOCATION_ID")
		}
	})

	_ = os.Unsetenv("INVOCATION_ID")
	if runningUnderSystemd() {
		t.Error("want false with INVOCATION_ID unset")
	}

	_ = os.Setenv("INVOCATION_ID", "deadbeefdeadbeefdeadbeefdeadbeef")
	if !runningUnderSystemd() {
		t.Error("want true with INVOCATION_ID set, matching how systemd marks every managed invocation")
	}
}
