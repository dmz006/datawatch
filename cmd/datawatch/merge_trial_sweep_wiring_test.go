package main

import (
	"os"
	"strings"
	"testing"
)

// The daemon sweeps leftover merge trials at start, before anything can start a
// new one: before the coordinator runs and before the executors are wired.
func TestDaemonStartSweepsLeftoverMergeTrials(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	sweep := strings.Index(src, "amgr.SweepStaleMergeTrials(")
	if sweep < 0 {
		t.Fatal("main.go does not sweep leftover merge trials at start")
	}
	for _, later := range []string{"autoCoord.Start()", "aAPI.SetExecutors("} {
		if at := strings.Index(src, later); at < 0 || at < sweep {
			t.Fatalf("the sweep must come before %s", later)
		}
	}
	if audit := strings.Index(src, "amgr.SetAuditFn("); audit < 0 || audit > sweep {
		t.Fatal("the sweep must come after the audit log is wired, so it is recorded")
	}
}
