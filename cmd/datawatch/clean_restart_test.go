package main

import (
	"context"
	"testing"
)

// TestCleanRestartSkipsRestore checks the spawn-time hook: with the clean
// restart flag set the restore is not called (capture still runs first); with
// it unset the restore is called with the last attempt ref.
func TestCleanRestartSkipsRestore(t *testing.T) {
	refs := []string{"refs/datawatch/attempts/t/1"}

	run := func(cleanRestart bool) []string {
		var calls []string
		capture := func() { calls = append(calls, "capture") }
		restore := func(ctx context.Context, dir, ref string) error {
			calls = append(calls, "restore:"+ref)
			return nil
		}
		restoreAttemptWorkBeforeSpawn(context.Background(), "/proj", refs, true, cleanRestart, capture, restore)
		return calls
	}

	if got := run(true); len(got) != 1 || got[0] != "capture" {
		t.Fatalf("clean restart: calls = %v, want only capture", got)
	}
	if got := run(false); len(got) != 2 || got[1] != "restore:refs/datawatch/attempts/t/1" {
		t.Fatalf("plain spawn: calls = %v, want capture then restore", got)
	}
}
