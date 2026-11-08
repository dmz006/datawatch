package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// verifyStep is one scripted verifier result. Once the script runs out, the
// verifier passes.
type verifyStep struct {
	res VerificationResult
	err error
}

// resumeFlowRun records what one executeOne run did through the fakes.
type resumeFlowRun struct {
	spawnCalls int
	sentSIDs   []string
	sentLines  []string
	verifySIDs []string
	// attempts is the task's stored attempt records after the run.
	attempts []TaskAttempt
	err      error
}

// runRetryResumeFlow drives executeOne through a first attempt that spawns a
// session, a scripted verifier and a retry, with the session liveness,
// context size and send-input hooks faked.
func runRetryResumeFlow(t *testing.T, maxTokens int, alive bool, ctxTokens int, sendErr error, script []verifyStep) resumeFlowRun {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.mu.Lock()
	m.cfg.RetryResumeMaxContextTokens = maxTokens
	m.mu.Unlock()

	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task", Backend: "claude-code"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)

	var out resumeFlowRun
	m.SetSessionAliveFn(func(sessionID string) bool { return alive })
	m.SetSessionContextTokensFn(func(sessionID string) int { return ctxTokens })
	m.SetSessionContinueLineFn(func(sessionID, line string) error {
		out.sentSIDs = append(out.sentSIDs, sessionID)
		out.sentLines = append(out.sentLines, line)
		return sendErr
	})

	spawnFn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		out.spawnCalls++
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", out.spawnCalls)}, nil
	}
	step := 0
	verifyFn := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		out.verifySIDs = append(out.verifySIDs, task.SessionID)
		if step < len(script) {
			s := script[step]
			step++
			if s.err != nil {
				return VerificationResult{}, s.err
			}
			return s.res, nil
		}
		return VerificationResult{OK: true, VerifiedAt: time.Now()}, nil
	}

	task := &prd.Story[0].Tasks[0]
	out.err = m.executeOne(context.Background(), prd, task, spawnFn, verifyFn, 2, pipeline.QualityGateConfig{}, nil)
	if stored, ok := m.Store().GetPRD(prd.ID); ok {
		if st, _ := findTaskInPRD(stored, task.ID); st != nil {
			out.attempts = st.Attempts
		}
	}
	return out
}

// failedVerification is a verifier result that fails with a summary.
func failedVerification(summary string) verifyStep {
	return verifyStep{res: VerificationResult{OK: false, Summary: summary}}
}

func TestRetryResumeFlow_ResumeSendsSummaryIntoSameSession(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, true, 5000, nil, []verifyStep{
		failedVerification("unit tests failed in pkg/widget"),
	})
	if run.err != nil {
		t.Fatalf("executeOne: %v", run.err)
	}
	if run.spawnCalls != 1 {
		t.Errorf("spawn calls = %d, want 1 (the retry must not start a new session)", run.spawnCalls)
	}
	if len(run.sentSIDs) != 1 || run.sentSIDs[0] != "sess-1" {
		t.Fatalf("sent into sessions %v, want exactly [sess-1]", run.sentSIDs)
	}
	line := run.sentLines[0]
	if !strings.Contains(line, retryResumeFraming) {
		t.Errorf("sent line lacks the resume framing: %q", line)
	}
	if !strings.Contains(line, "unit tests failed in pkg/widget") {
		t.Errorf("sent line lacks the failure summary: %q", line)
	}
	if len(run.verifySIDs) != 2 || run.verifySIDs[1] != "sess-1" {
		t.Errorf("verify session ids = %v, want the retry to wait on sess-1", run.verifySIDs)
	}
}

func TestRetryResumeFlow_SessionEndedStartsFresh(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, false, 5000, nil, []verifyStep{
		failedVerification("tests failed"),
	})
	if run.spawnCalls != 2 {
		t.Errorf("spawn calls = %d, want 2 (a fresh session for the retry)", run.spawnCalls)
	}
	if len(run.sentSIDs) != 0 {
		t.Errorf("nothing must be sent into an ended session, sent into %v", run.sentSIDs)
	}
}

func TestRetryResumeFlow_ContextAboveLimitStartsFresh(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, true, 150001, nil, []verifyStep{
		failedVerification("tests failed"),
	})
	if run.spawnCalls != 2 {
		t.Errorf("spawn calls = %d, want 2 (context above the limit starts fresh)", run.spawnCalls)
	}
	if len(run.sentSIDs) != 0 {
		t.Errorf("nothing must be sent when the context is above the limit, sent into %v", run.sentSIDs)
	}
}

func TestRetryResumeFlow_ZeroSettingStartsFresh(t *testing.T) {
	run := runRetryResumeFlow(t, 0, true, 0, nil, []verifyStep{
		failedVerification("tests failed"),
	})
	if run.spawnCalls != 2 {
		t.Errorf("spawn calls = %d, want 2 (setting 0 starts fresh)", run.spawnCalls)
	}
	if len(run.sentSIDs) != 0 {
		t.Errorf("nothing must be sent with the setting at 0, sent into %v", run.sentSIDs)
	}
}

func TestRetryResumeFlow_StallStartsFresh(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, true, 5000, nil, []verifyStep{
		{err: fmt.Errorf("worker went quiet: %w", ErrWorkerStalled)},
	})
	if run.err != nil {
		t.Fatalf("executeOne: %v", run.err)
	}
	if run.spawnCalls != 2 {
		t.Errorf("spawn calls = %d, want 2 (a stall starts fresh, never resumes)", run.spawnCalls)
	}
	if len(run.sentSIDs) != 0 {
		t.Errorf("a stalled session must not be sent into, sent into %v", run.sentSIDs)
	}
}

func TestRetryResumeFlow_SendFailureStartsFresh(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, true, 5000, errors.New("pane gone"), []verifyStep{
		failedVerification("tests failed"),
	})
	if run.spawnCalls != 2 {
		t.Errorf("spawn calls = %d, want 2 (a failed send falls back to a fresh session)", run.spawnCalls)
	}
}

func TestRetryResumeLine_FlattensNewlinesAndCaps(t *testing.T) {
	line := retryResumeLine("first line\nsecond line\r\nthird")
	if strings.ContainsAny(line, "\r\n") {
		t.Errorf("line must be single-line, got %q", line)
	}
	if !strings.Contains(line, "first line | second line | third") {
		t.Errorf("newlines not flattened: %q", line)
	}
	long := retryResumeLine(strings.Repeat("x", retryResumeMaxLineBytes*2))
	if !strings.HasSuffix(long, escalationTruncationMarker) {
		t.Errorf("an oversized summary must be capped with the truncation marker")
	}
}
