// B98 — tests for the opencode usage-tracking helpers.

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFakeOpenCodeBinary writes a shell script standing in for the real
// opencode CLI, returning a `session list` entry and `export` payload
// driven by env vars so a test can change the "cumulative totals"
// between calls without rewriting the script.
func writeFakeOpenCodeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "session" ] && [ "$2" = "list" ]; then
  printf '[{"id":"ses_test123","directory":"%s","created":%s}]' "$FAKE_DIR" "$FAKE_CREATED"
elif [ "$1" = "export" ]; then
  printf 'Exporting session: %s\n{"info":{"tokens":{"input":%s,"output":%s}}}' "$2" "$FAKE_IN" "$FAKE_OUT"
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test fixture needs to be executable
		t.Fatal(err)
	}
	return path
}

func TestResolveOpenCodeSessionID_MatchesDirectoryAndClosestTime(t *testing.T) {
	binary := writeFakeOpenCodeBinary(t)
	createdAt := time.UnixMilli(1000000)
	t.Setenv("FAKE_DIR", "/proj/a")
	t.Setenv("FAKE_CREATED", "1000000")

	got := resolveOpenCodeSessionID(context.Background(), binary, "/proj/a", createdAt)
	if got != "ses_test123" {
		t.Fatalf("got %q, want ses_test123", got)
	}
}

func TestResolveOpenCodeSessionID_NoDirectoryMatchReturnsEmpty(t *testing.T) {
	binary := writeFakeOpenCodeBinary(t)
	t.Setenv("FAKE_DIR", "/proj/a")
	t.Setenv("FAKE_CREATED", "1000000")

	got := resolveOpenCodeSessionID(context.Background(), binary, "/proj/other", time.UnixMilli(1000000))
	if got != "" {
		t.Fatalf("got %q, want empty (no directory match)", got)
	}
}

func TestResolveOpenCodeSessionID_EmptyProjectDirIsNoop(t *testing.T) {
	binary := writeFakeOpenCodeBinary(t)
	if got := resolveOpenCodeSessionID(context.Background(), binary, "", time.Now()); got != "" {
		t.Fatalf("empty project dir should yield empty, got %q", got)
	}
}

func TestScanOpenCodeUsageOnce_SumsDeltaAcrossTicks(t *testing.T) {
	binary := writeFakeOpenCodeBinary(t)
	t.Setenv("FAKE_DIR", "/proj/b")
	t.Setenv("FAKE_CREATED", "2000000")

	const sessID = "sess-opencode-delta-test"
	t.Cleanup(func() {
		opencodeSessionIDs.Delete(sessID)
		opencodeLastTotals.Delete(sessID)
	})

	var gotIn, gotOut, calls int
	report := func(_ string, tokensIn, tokensOut int) {
		calls++
		gotIn += tokensIn
		gotOut += tokensOut
	}

	t.Setenv("FAKE_IN", "100")
	t.Setenv("FAKE_OUT", "20")
	scanOpenCodeUsageOnce(context.Background(), binary, sessID, "/proj/b", time.UnixMilli(2000000), report)
	if calls != 1 || gotIn != 100 || gotOut != 20 {
		t.Fatalf("first scan: calls=%d in=%d out=%d, want 1/100/20", calls, gotIn, gotOut)
	}

	// No change in cumulative totals -- must not report again.
	scanOpenCodeUsageOnce(context.Background(), binary, sessID, "/proj/b", time.UnixMilli(2000000), report)
	if calls != 1 {
		t.Fatalf("unchanged cumulative totals should not call report again: calls=%d", calls)
	}

	// Cumulative totals advance -- only the delta should be reported.
	t.Setenv("FAKE_IN", "150")
	t.Setenv("FAKE_OUT", "35")
	scanOpenCodeUsageOnce(context.Background(), binary, sessID, "/proj/b", time.UnixMilli(2000000), report)
	if calls != 2 || gotIn != 150 || gotOut != 35 {
		t.Fatalf("after advance: calls=%d in=%d out=%d, want 2/150/35 (100+50, 20+15)", calls, gotIn, gotOut)
	}

	if v, ok := opencodeSessionIDs.Load(sessID); !ok || v.(string) != "ses_test123" {
		t.Errorf("resolved session ID should be cached: %v, %v", v, ok)
	}
}

func TestTrackOpenCodeUsage_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	const sessID = "sess-opencode-cancel-test"
	go func() {
		trackOpenCodeUsage(ctx, sessID, "/some/project", time.Now(), 5*time.Millisecond, func(string, int, int) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("trackOpenCodeUsage did not stop after context cancel")
	}
	if _, ok := opencodeSessionIDs.Load(sessID); ok {
		t.Error("opencodeSessionIDs entry should be cleaned up on return")
	}
	if _, ok := opencodeLastTotals.Load(sessID); ok {
		t.Error("opencodeLastTotals entry should be cleaned up on return")
	}
}

func TestTrackOpenCodeUsage_NilReportFnIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		trackOpenCodeUsage(ctx, "sess-opencode-nil-test", "/some/project", time.Now(), time.Millisecond, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trackOpenCodeUsage with a nil reportFn should return immediately")
	}
}
