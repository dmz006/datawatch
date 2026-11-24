package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subagentFixtureDir returns where Claude Code writes a session's subagent
// transcripts and points HOME at a temp dir so nothing real is read.
func subagentFixtureDir(t *testing.T, projectDir, fullID string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(filepath.Dir(claudeTranscriptPath(projectDir, fullID)), claudeSessionUUID(fullID), "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeSubagentFile(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendSubagentFile(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

// A supervisor session's subagents write their own transcripts and have no
// session record, so their tokens never reached the parent's totals, the cost
// summary or the spend ledger (about 2.6 million tokens in one afternoon).
func TestSubagentUsageRecordedUnderParentSession(t *testing.T) {
	const project = "/work/supervisor-proj"
	mgr, led := newLedgerManager(t)
	sess := &Session{FullID: "host-sa1", ID: "sa1", BackendFamily: "claude-code", State: StateRunning, ProjectDir: project}
	dir := subagentFixtureDir(t, project, sess.FullID)
	mgr.store.mu.Lock()
	mgr.store.sessions[sess.FullID] = sess
	mgr.store.mu.Unlock()
	t.Cleanup(func() { claudeSubagentState.Delete(sess.FullID) })

	// The parent's own transcript has already been counted.
	if err := mgr.AddClaudeUsage(sess.FullID, ClaudeUsageDelta{In: 10, Out: 20, LinesRead: 4}); err != nil {
		t.Fatal(err)
	}
	// Two subagents; message m1 is written on two lines (counted once).
	writeSubagentFile(t, dir, "agent-aaa.jsonl",
		`{"type":"fork-context-ref","agentId":"aaa"}`,
		`{"type":"assistant","isSidechain":true,"agentId":"aaa","message":{"id":"m1","usage":{"input_tokens":5,"output_tokens":50,"cache_read_input_tokens":500,"cache_creation_input_tokens":40},"content":[{"type":"tool_use","id":"t1"}]}}`,
		`{"type":"assistant","isSidechain":true,"agentId":"aaa","message":{"id":"m1","usage":{"input_tokens":5,"output_tokens":50,"cache_read_input_tokens":500,"cache_creation_input_tokens":40},"content":[{"type":"tool_use","id":"t2"}]}}`,
	)
	writeSubagentFile(t, dir, "agent-bbb.jsonl",
		`{"type":"assistant","isSidechain":true,"agentId":"bbb","message":{"id":"m2","usage":{"input_tokens":3,"output_tokens":30,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[]}}`,
	)

	if err := mgr.ScanSubagentUsage(sess.FullID); err != nil {
		t.Fatal(err)
	}
	// A second scan with nothing new must not count anything again.
	if err := mgr.ScanSubagentUsage(sess.FullID); err != nil {
		t.Fatal(err)
	}
	got, _ := mgr.GetSession(sess.FullID)
	if got.TokensIn != 18 || got.TokensOut != 100 || got.CacheReadTokens != 500 || got.CacheCreationTokens != 40 || got.ToolCalls != 2 {
		t.Fatalf("parent totals %+v: want in 18 out 100 cache 500/40 tools 2 (own 10/20 plus subagents 8/80)", got)
	}
	if got.SubagentTokensIn != 8 || got.SubagentTokensOut != 80 || got.SubagentToolCalls != 2 {
		t.Fatalf("subagent share in %d out %d tools %d, want 8/80/2", got.SubagentTokensIn, got.SubagentTokensOut, got.SubagentToolCalls)
	}
	if got.EstCostUSD <= 0 {
		t.Fatalf("subagent tokens were not priced: est cost %v", got.EstCostUSD)
	}
	if e := led.last(t); e.TokensIn != 18 || e.TokensOut != 100 {
		t.Fatalf("ledger entry %+v does not carry the subagent tokens", e)
	}
	if sum := SummaryFor(mgr.ListSessions()); sum.TotalIn != 18 || sum.TotalOut != 100 || sum.SubagentIn != 8 || sum.SubagentOut != 80 {
		t.Fatalf("cost summary %+v", sum)
	}

	// New lines on a later tick, plus a restart (state dropped, checkpoints
	// reloaded from the session record), still count only what is new.
	appendSubagentFile(t, dir, "agent-bbb.jsonl",
		`{"type":"assistant","isSidechain":true,"agentId":"bbb","message":{"id":"m3","usage":{"input_tokens":1,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[]}}`)
	claudeSubagentState.Delete(sess.FullID)
	if err := mgr.ScanSubagentUsage(sess.FullID); err != nil {
		t.Fatal(err)
	}
	got, _ = mgr.GetSession(sess.FullID)
	if got.TokensIn != 19 || got.TokensOut != 110 || got.SubagentTokensIn != 9 || got.SubagentTokensOut != 90 {
		t.Fatalf("after restart totals in %d out %d (subagent %d/%d), want 19/110 (9/90)", got.TokensIn, got.TokensOut, got.SubagentTokensIn, got.SubagentTokensOut)
	}
}
