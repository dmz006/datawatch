// B98 — tests for the claude-code JSONL usage-tracking helpers.

package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestScanClaudeUsageOnce_CacheTokensNotCounted(t *testing.T) {
	// GH#98 deliberate scope limit: cache_creation_input_tokens /
	// cache_read_input_tokens are billed at very different rates than
	// base input tokens, and CostRate has no cache-aware field --
	// folding them in would silently produce a wildly wrong dollar
	// figure. Confirm they're ignored, not just "not implemented yet
	// but accidentally summed".
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"type":"assistant","message":{"usage":{"input_tokens":2,"output_tokens":136,"cache_creation_input_tokens":30745,"cache_read_input_tokens":9893}}}` + "\n"
	if _, err := f.WriteString(raw); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Unique session ID + explicit cleanup -- claudeUsageLinesRead is
	// package-level state shared across every test in this file AND
	// across -count=N repeats of this same test within one process;
	// without both, a later run picks up a stale "already read" count
	// and skips this file's only line as "already seen".
	const sessID = "sess-cache-test"
	t.Cleanup(func() { claudeUsageLinesRead.Delete(sessID) })
	var gotIn, gotOut int
	scanClaudeUsageOnce(path, sessID, func(sessID string, tokensIn, tokensOut, linesRead int) {
		gotIn, gotOut = tokensIn, tokensOut
	})
	if gotIn != 2 || gotOut != 136 {
		t.Fatalf("got in=%d out=%d, want in=2 out=136 (cache tokens must not be folded in)", gotIn, gotOut)
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

func writeFixtureTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func scanFixture(t *testing.T, id, path string) ClaudeUsageDelta {
	t.Helper()
	t.Cleanup(func() { claudeUsageLinesRead.Delete(id); claudeUsageSeen.Delete(id) })
	var got ClaudeUsageDelta
	scanClaudeUsageDeltaOnce(path, id, func(_ string, d ClaudeUsageDelta) { got = d })
	return got
}

func TestScanClaudeUsageDelta_CacheDedupAndToolCalls(t *testing.T) {
	path := writeFixtureTranscript(t,
		`{"type":"user","message":{"content":"hi"}}`,
		// message m1 is written on two lines with identical usage; each line carries one tool_use
		`{"type":"assistant","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":30},"content":[{"type":"text","text":"x"},{"type":"tool_use","id":"t1"}]}}`,
		`{"type":"assistant","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":30},"content":[{"type":"tool_use","id":"t2"}]}}`,
		`{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":1,"output_tokens":5,"cache_read_input_tokens":50,"cache_creation_input_tokens":0},"content":[{"type":"tool_use","id":"t3"},{"type":"tool_use","id":"t3"}]}}`,
		`not json`,
	)
	got := scanFixture(t, "sess-cache-dedup-fixture", path)
	want := ClaudeUsageDelta{In: 3, Out: 15, CacheRead: 150, CacheCreation: 30, ToolCalls: 3, LinesRead: 5}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestScanClaudeUsageDelta_DedupAcrossTicks(t *testing.T) {
	const id = "sess-dedup-ticks-fixture"
	line := `{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":4,"output_tokens":6,"cache_read_input_tokens":7,"cache_creation_input_tokens":8},"content":[]}}`
	path := writeFixtureTranscript(t, line)
	t.Cleanup(func() { claudeUsageLinesRead.Delete(id); claudeUsageSeen.Delete(id) })
	var total ClaudeUsageDelta
	rep := func(_ string, d ClaudeUsageDelta) {
		total.In += d.In
		total.CacheRead += d.CacheRead
		total.CacheCreation += d.CacheCreation
	}
	scanClaudeUsageDeltaOnce(path, id, rep)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(line + "\n") // same message id again on a later tick
	_ = f.Close()
	scanClaudeUsageDeltaOnce(path, id, rep)
	if total.In != 4 || total.CacheRead != 7 || total.CacheCreation != 8 {
		t.Fatalf("duplicate id re-counted across ticks: %+v", total)
	}
}

func TestAddClaudeUsage_FeedsLedger(t *testing.T) {
	mgr, led := newLedgerManager(t)
	sess := &Session{FullID: "host-cu1", ID: "cu1", BackendFamily: "claude-code", State: StateRunning}
	mgr.store.mu.Lock()
	mgr.store.sessions[sess.FullID] = sess
	mgr.store.mu.Unlock()
	if err := mgr.AddClaudeUsage(sess.FullID, ClaudeUsageDelta{In: 1, Out: 2, CacheRead: 30, CacheCreation: 4, ToolCalls: 5, LinesRead: 9}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddClaudeUsage(sess.FullID, ClaudeUsageDelta{CacheRead: 10, ToolCalls: 1, LinesRead: 12}); err != nil {
		t.Fatal(err)
	}
	e := led.last(t)
	if e.TokensIn != 1 || e.TokensOut != 2 || e.CacheRead != 40 || e.CacheCreation != 4 || e.ToolCalls != 6 {
		t.Fatalf("ledger entry %+v", e)
	}
}
