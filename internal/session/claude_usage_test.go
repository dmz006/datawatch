// B98 — tests for the claude-code JSONL usage-tracking helpers.

package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeSessionUUID_Deterministic(t *testing.T) {
	a := claudeSessionUUID("h-session-1")
	b := claudeSessionUUID("h-session-1")
	if a != b {
		t.Fatalf("deriveSessionUUID must be deterministic: %q != %q", a, b)
	}
	if claudeSessionUUID("h-session-1") == claudeSessionUUID("h-session-2") {
		t.Fatal("different session IDs must not collide")
	}
	// UUID v5 shape: 8-4-4-4-12 hex, version nibble 5, variant bits set.
	if len(a) != 36 || a[14] != '5' {
		t.Fatalf("not a well-formed UUID v5: %q", a)
	}
}

func TestClaudeTranscriptPath_EscapesSlashesAndDots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := claudeTranscriptPath("/home/<user>/workspace/datawatch/.hllm-sandbox", "full-id")
	want := filepath.Join(home, ".claude", "projects", "-home-<user>-workspace-datawatch--hllm-sandbox", claudeSessionUUID("full-id")+".jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClaudeTranscriptPath_EmptyProjectDir(t *testing.T) {
	if got := claudeTranscriptPath("", "full-id"); got != "" {
		t.Errorf("empty project dir should yield empty path, got %q", got)
	}
}

func writeJSONLLine(t *testing.T, f *os.File, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestScanClaudeUsageOnce_SumsOnlyNewAssistantLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}

	type usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	type msg struct {
		Usage usage `json:"usage"`
	}
	type line struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}

	writeJSONLLine(t, f, line{Type: "user"}) // no usage, must contribute nothing
	writeJSONLLine(t, f, line{Type: "assistant", Message: msg{Usage: usage{InputTokens: 10, OutputTokens: 5}}})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// scanClaudeUsageOnce (unlike trackClaudeCodeUsage) has no deferred
	// cleanup of its own -- clean up explicitly so claudeUsageLinesRead
	// (package-level, shared across -count=N repeats of this same test
	// within one process) never leaks a stale "already read" count into
	// a later run.
	const sessID = "sess-sums-test"
	t.Cleanup(func() { claudeUsageLinesRead.Delete(sessID) })

	var gotIn, gotOut int
	calls := 0
	report := func(sessID string, tokensIn, tokensOut, linesRead int) {
		calls++
		gotIn += tokensIn
		gotOut += tokensOut
	}

	scanClaudeUsageOnce(path, sessID, report)
	if calls != 1 || gotIn != 10 || gotOut != 5 {
		t.Fatalf("first scan: calls=%d in=%d out=%d, want 1/10/5", calls, gotIn, gotOut)
	}

	// Second scan with no new lines appended must not double-count.
	scanClaudeUsageOnce(path, sessID, report)
	if calls != 1 {
		t.Fatalf("second scan with no new data should not call report again: calls=%d", calls)
	}

	// Append a new turn -- only the delta should be reported.
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	writeJSONLLine(t, f, line{Type: "assistant", Message: msg{Usage: usage{InputTokens: 3, OutputTokens: 7}}})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	scanClaudeUsageOnce(path, sessID, report)
	if calls != 2 || gotIn != 13 || gotOut != 12 {
		t.Fatalf("after append: calls=%d in=%d out=%d, want 2/13/12 (10+3 in, 5+7 out)", calls, gotIn, gotOut)
	}
}

func TestScanClaudeUsageOnce_MissingFileIsNoop(t *testing.T) {
	called := false
	scanClaudeUsageOnce(filepath.Join(t.TempDir(), "does-not-exist.jsonl"), "sess-missing-test", func(string, int, int, int) { called = true })
	if called {
		t.Error("a missing transcript file must not call report")
	}
}

func TestScanClaudeUsage_CacheTokensCountedApart(t *testing.T) {
	// Cache tokens are reported through their own callback and never folded
	// into the input count; without a callback they are simply not reported.
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	raw := `{"type":"assistant","message":{"usage":{"input_tokens":2,"output_tokens":136,"cache_creation_input_tokens":30745,"cache_read_input_tokens":9893}}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	const sessID = "sess-cache-test"
	t.Cleanup(func() { claudeUsageLinesRead.Delete(sessID) })
	var gotIn, gotOut, gotCR, gotCW int
	scanClaudeUsage(path, sessID, func(_ string, tokensIn, tokensOut, _ int) {
		gotIn, gotOut = tokensIn, tokensOut
	}, func(_ string, cr, cw int) { gotCR, gotCW = cr, cw })
	if gotIn != 2 || gotOut != 136 {
		t.Fatalf("got in=%d out=%d, want in=2 out=136 (cache tokens must not be folded in)", gotIn, gotOut)
	}
	if gotCR != 9893 || gotCW != 30745 {
		t.Fatalf("got cache read=%d write=%d, want 9893 / 30745", gotCR, gotCW)
	}

	// Nil cache callback: scanning still works and reports no cache.
	claudeUsageLinesRead.Delete(sessID)
	gotIn = 0
	scanClaudeUsageOnce(path, sessID, func(_ string, tokensIn, _, _ int) { gotIn = tokensIn })
	if gotIn != 2 {
		t.Fatalf("scanClaudeUsageOnce in=%d, want 2", gotIn)
	}
}

func TestAddCacheUsage_AccumulatesAndRejectsUnknown(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	if err := mgr.AddCacheUsage("nope", 1, 1); err == nil {
		t.Fatal("unknown session must fail")
	}
	if err := mgr.SaveSession(&Session{ID: "c1", FullID: "h-c1"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := mgr.AddCacheUsage("c1", 10, 5); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := mgr.GetSession("c1")
	if !ok {
		t.Fatal("session vanished")
	}
	if got.CacheReadTokens != 20 || got.CacheWriteTokens != 10 {
		t.Fatalf("cache totals = %d/%d, want 20/10", got.CacheReadTokens, got.CacheWriteTokens)
	}
}

func TestTrackClaudeCodeUsage_StopsOnContextCancel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		trackClaudeCodeUsage(ctx, "sess-cancel-test", "/some/project", 0, 5*time.Millisecond, func(string, int, int, int) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("trackClaudeCodeUsage did not stop after context cancel")
	}
	if _, ok := claudeUsageLinesRead.Load("sess-cancel-test"); ok {
		t.Error("claudeUsageLinesRead entry should be cleaned up on return")
	}
}

func TestTrackClaudeCodeUsage_EmptyProjectDirIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	done := make(chan struct{})
	go func() {
		trackClaudeCodeUsage(ctx, "sess-1", "", 0, time.Millisecond, func(string, int, int, int) { called = true })
		close(done)
	}()
	// Should return immediately (empty path) rather than looping.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trackClaudeCodeUsage with an empty project dir should return immediately")
	}
	if called {
		t.Error("should never call reportFn when there's no path to read")
	}
}
