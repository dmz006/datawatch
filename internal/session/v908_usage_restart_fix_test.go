// v9.0.8 — regression tests for the usage-tracker restart-amplification
// bug: claudeUsageLinesRead/aiderUsageLinesRead/opencodeLastTotals/
// gooseLastTotals used to be in-memory-only, so a fresh daemon process
// (a restart) reset the "how much have I already counted" checkpoint to
// zero. The next scan tick then re-read/re-diffed a session's ENTIRE
// transcript/cumulative-total from scratch and reported that whole
// historical sum again, which AddUsage added on top of the session's
// already-persisted total -- a session that survived N restarts had its
// real usage multiplied by roughly N+1. Found live: a claude-code
// session reporting 4.3B output tokens / $64,532 that had never
// plausibly spent that much.
//
// The fix persists the checkpoint on the Session record itself
// (UsageLinesRead / UsageLastIn / UsageLastOut) and seeds each tracker
// from it at goroutine start, instead of always starting from zero.
package session

import (
	"os"
	"testing"
)

// TestAddUsageLinesRead_PersistsCheckpointWithTokens confirms the
// combined save actually writes both the token delta AND the new
// checkpoint in one go -- not just the tokens.
func TestAddUsageLinesRead_PersistsCheckpointWithTokens(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "cc01", FullID: "testhost-cc01", BackendFamily: "claude-code",
	})

	if err := mgr.AddUsageLinesRead("testhost-cc01", 10, 5, 2); err != nil {
		t.Fatalf("AddUsageLinesRead: %v", err)
	}
	sess, ok := mgr.store.Get("testhost-cc01")
	if !ok {
		t.Fatal("session vanished")
	}
	if sess.TokensIn != 10 || sess.TokensOut != 5 {
		t.Errorf("tokens = %d/%d, want 10/5", sess.TokensIn, sess.TokensOut)
	}
	if sess.UsageLinesRead != 2 {
		t.Errorf("UsageLinesRead = %d, want 2 (checkpoint must persist alongside the token delta)", sess.UsageLinesRead)
	}
}

// TestAddUsageLastTotals_PersistsCheckpointWithTokens is the
// opencode/goose-shaped equivalent of the above.
func TestAddUsageLastTotals_PersistsCheckpointWithTokens(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "oc01", FullID: "testhost-oc01", BackendFamily: "opencode",
	})

	if err := mgr.AddUsageLastTotals("testhost-oc01", 100, 20, 100, 20); err != nil {
		t.Fatalf("AddUsageLastTotals: %v", err)
	}
	sess, ok := mgr.store.Get("testhost-oc01")
	if !ok {
		t.Fatal("session vanished")
	}
	if sess.TokensIn != 100 || sess.TokensOut != 20 {
		t.Errorf("tokens = %d/%d, want 100/20", sess.TokensIn, sess.TokensOut)
	}
	if sess.UsageLastIn != 100 || sess.UsageLastOut != 20 {
		t.Errorf("UsageLastIn/Out = %d/%d, want 100/20", sess.UsageLastIn, sess.UsageLastOut)
	}
}

// TestScanClaudeUsageOnce_RestartDoesNotReCountHistory is the actual
// regression test for the incident: a "restart" (a fresh call with the
// persisted checkpoint seeded in, exactly as manager.go now does from
// sess.UsageLinesRead) must not re-report lines already counted before
// the restart.
func TestScanClaudeUsageOnce_RestartDoesNotReCountHistory(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/transcript.jsonl"
	writeTestTranscriptLines(t, path, []string{
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":3,"output_tokens":7}}}`,
	})

	const sessID = "sess-restart-test"
	t.Cleanup(func() { claudeUsageLinesRead.Delete(sessID) })

	// "Generation 1": daemon boots fresh, scans, persists checkpoint=2
	// (simulating what manager.go would do: initial checkpoint 0, since
	// this is the session's first-ever scan).
	claudeUsageLinesRead.Store(sessID, 0)
	var gen1In, gen1Out, persistedCheckpoint int
	scanClaudeUsageOnce(path, sessID, func(_ string, in, out, linesRead int) {
		gen1In, gen1Out = in, out
		persistedCheckpoint = linesRead
	})
	if gen1In != 13 || gen1Out != 12 {
		t.Fatalf("gen1: got in=%d out=%d, want 13/12", gen1In, gen1Out)
	}
	if persistedCheckpoint != 2 {
		t.Fatalf("gen1: checkpoint = %d, want 2", persistedCheckpoint)
	}

	// Simulate a daemon restart: wipe the in-memory map (a fresh process
	// would never have this entry at all) and re-seed it from the
	// PERSISTED checkpoint instead of zero -- exactly what manager.go's
	// `trackClaudeCodeUsage(ctx, ..., sess.UsageLinesRead, ...)` call
	// does on every session reattach.
	claudeUsageLinesRead.Delete(sessID)
	claudeUsageLinesRead.Store(sessID, persistedCheckpoint)

	// "Generation 2": no new transcript lines were written. The old,
	// unfixed behavior (seeding from 0 every restart) would re-read and
	// re-report the same 13/12 tokens again here -- the whole point of
	// this test is confirming that does NOT happen.
	called := false
	scanClaudeUsageOnce(path, sessID, func(_ string, in, out, linesRead int) {
		called = true
	})
	if called {
		t.Fatal("restart re-reported already-counted history -- this is exactly the amplification bug the fix must prevent")
	}
}

// TestResetPreFixUsageIfCorrupted_OnlyTouchesTheAmplifiedCase confirms
// the self-heal's exact targeting: it must reset a session that has
// usage but no checkpoint (the signature unique to pre-fix corruption),
// and must leave alone both a session with no usage at all and a
// session that already has a real checkpoint (post-fix, correctly
// tracked, whatever its token count happens to be).
func TestResetPreFixUsageIfCorrupted_OnlyTouchesTheAmplifiedCase(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	corrupted := &Session{FullID: "testhost-corrupted", BackendFamily: "claude-code", TokensIn: 4299603318, TokensOut: 12884822, EstCostUSD: 64532.70}
	_ = mgr.SaveSession(corrupted)
	mgr.resetPreFixUsageIfCorrupted(corrupted)
	if got, ok := mgr.store.Get("testhost-corrupted"); !ok || got.TokensIn != 0 || got.TokensOut != 0 || got.EstCostUSD != 0 {
		t.Errorf("corrupted session (usage but no checkpoint) should be reset to zero, got %+v", got)
	}

	neverUsed := &Session{FullID: "testhost-never-used", BackendFamily: "claude-code"}
	_ = mgr.SaveSession(neverUsed)
	mgr.resetPreFixUsageIfCorrupted(neverUsed)
	if got, ok := mgr.store.Get("testhost-never-used"); !ok || got.TokensIn != 0 {
		t.Errorf("a session with no usage at all should stay untouched (trivially zero either way), got %+v", got)
	}

	healthy := &Session{FullID: "testhost-healthy", BackendFamily: "claude-code", TokensIn: 500, TokensOut: 200, EstCostUSD: 0.0045, UsageLinesRead: 7}
	_ = mgr.SaveSession(healthy)
	mgr.resetPreFixUsageIfCorrupted(healthy)
	if got, ok := mgr.store.Get("testhost-healthy"); !ok || got.TokensIn != 500 || got.TokensOut != 200 {
		t.Errorf("a session with a real checkpoint (UsageLinesRead != 0) must never be reset, got %+v", got)
	}
}

func writeTestTranscriptLines(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
