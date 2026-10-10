// B98 — tests for the goose usage-tracking helpers.

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFakeGooseBinary writes a shell script standing in for the real
// goose CLI's `session export --name <name> --format json`, returning
// cumulative totals driven by env vars.
func writeFakeGooseBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "goose")
	script := `#!/bin/sh
if [ "$1" = "session" ] && [ "$2" = "export" ]; then
  printf '{"accumulated_usage":{"input_tokens":%s,"output_tokens":%s}}' "$FAKE_IN" "$FAKE_OUT"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test fixture needs to be executable
		t.Fatal(err)
	}
	return path
}

func TestScanGooseUsageOnce_SumsDeltaAcrossTicks(t *testing.T) {
	binary := writeFakeGooseBinary(t)
	const sessID = "sess-goose-delta-test"
	t.Cleanup(func() { gooseLastTotals.Delete(sessID) })

	var gotIn, gotOut, calls int
	report := func(_ string, tokensIn, tokensOut, curIn, curOut int) {
		calls++
		gotIn += tokensIn
		gotOut += tokensOut
	}

	t.Setenv("FAKE_IN", "200")
	t.Setenv("FAKE_OUT", "40")
	scanGooseUsageOnce(context.Background(), binary, sessID, "dw-test-session", report)
	if calls != 1 || gotIn != 200 || gotOut != 40 {
		t.Fatalf("first scan: calls=%d in=%d out=%d, want 1/200/40", calls, gotIn, gotOut)
	}

	// No change in cumulative totals -- must not report again.
	scanGooseUsageOnce(context.Background(), binary, sessID, "dw-test-session", report)
	if calls != 1 {
		t.Fatalf("unchanged cumulative totals should not call report again: calls=%d", calls)
	}

	// Cumulative totals advance -- only the delta should be reported.
	t.Setenv("FAKE_IN", "260")
	t.Setenv("FAKE_OUT", "55")
	scanGooseUsageOnce(context.Background(), binary, sessID, "dw-test-session", report)
	if calls != 2 || gotIn != 260 || gotOut != 55 {
		t.Fatalf("after advance: calls=%d in=%d out=%d, want 2/260/55 (200+60, 40+15)", calls, gotIn, gotOut)
	}
}

func TestTrackGooseUsage_EmptyNameIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	done := make(chan struct{})
	go func() {
		trackGooseUsage(ctx, "sess-goose-empty-name-test", "", 0, 0, time.Millisecond, func(string, int, int, int, int) { called = true })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trackGooseUsage with an empty session name should return immediately")
	}
	if called {
		t.Error("should never call reportFn when there's no name to look up")
	}
}

func TestTrackGooseUsage_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	const sessID = "sess-goose-cancel-test"
	go func() {
		trackGooseUsage(ctx, sessID, "dw-cancel-test", 0, 0, 5*time.Millisecond, func(string, int, int, int, int) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("trackGooseUsage did not stop after context cancel")
	}
	if _, ok := gooseLastTotals.Load(sessID); ok {
		t.Error("gooseLastTotals entry should be cleaned up on return")
	}
}
