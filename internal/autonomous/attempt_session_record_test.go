package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// recordOne records one failure on a fresh task and returns the attempt.
func recordOne(t *testing.T, mutate func(*Task), run attemptRun, f attemptFailure) TaskAttempt {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := &prd.Story[0].Tasks[0]
	if mutate != nil {
		mutate(task)
	}
	m.recordFailedAttempt(context.Background(), prd, task, run, f)
	if len(task.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(task.Attempts))
	}
	return task.Attempts[0]
}

func TestAttemptSessionRecord_SessionFromRunOnly(t *testing.T) {
	// A stale task session that this attempt did not start is not recorded.
	a := recordOne(t, func(tk *Task) { tk.SessionID = "sess-task" }, attemptRun{}, attemptFailure{Outcome: attemptOutcomeFailed})
	if a.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty: the attempt started no session", a.SessionID)
	}
	a = recordOne(t, func(tk *Task) { tk.SessionID = "sess-task" }, attemptRun{SessionID: "sess-run", TmuxSession: "dw-x"}, attemptFailure{Outcome: attemptOutcomeFailed})
	if a.SessionID != "sess-run" || a.TmuxSession != "dw-x" {
		t.Fatalf("got %q/%q, want run values", a.SessionID, a.TmuxSession)
	}
	a = recordOne(t, nil, attemptRun{}, attemptFailure{Outcome: attemptOutcomeFailed, Category: "infra"})
	if a.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty when none started", a.SessionID)
	}
}

func TestAttemptSessionRecord_EndReason(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*Task)
		f      attemptFailure
		want   string
		detail string
	}{
		{"explicit", nil, attemptFailure{Outcome: attemptOutcomeFailed, EndReason: attemptEndLaunchRefused, EndDetail: "draining"}, attemptEndLaunchRefused, "draining"},
		{"stalled", nil, attemptFailure{Outcome: attemptOutcomeStalled}, attemptEndStalled, ""},
		{"infra", nil, attemptFailure{Outcome: attemptOutcomeFailed, Category: "infra"}, attemptEndSessionDied, ""},
		{"verifier", nil, attemptFailure{Outcome: attemptOutcomeFailed, Category: "content", Verification: VerificationResult{Summary: "bad"}}, attemptEndVerifierFailed, ""},
		{"cancelled", nil, attemptFailure{Outcome: "cancelled"}, attemptEndCancelledByOperator, ""},
		{"generic", nil, attemptFailure{Outcome: attemptOutcomeFailed}, attemptEndExecutorError, ""},
		{"generic-with-category", nil, attemptFailure{Outcome: attemptOutcomeFailed, Category: "content"}, attemptEndExecutorError, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := recordOne(t, c.mut, attemptRun{}, c.f)
			if a.EndReason != c.want || a.EndDetail != c.detail {
				t.Fatalf("got %q/%q, want %q/%q", a.EndReason, a.EndDetail, c.want, c.detail)
			}
		})
	}
}

func TestAttemptSessionRecord_OldJSONLoadsAndOmitsEmpty(t *testing.T) {
	var a TaskAttempt
	if err := json.Unmarshal([]byte(`{"number":2,"outcome":"failed","failure_summary":"x"}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Number != 2 || a.SessionID != "" || a.EndReason != "" {
		t.Fatalf("old record loaded wrong: %+v", a)
	}
	out, _ := json.Marshal(a)
	for _, k := range []string{"session_id", "tmux_session", "end_reason", "end_detail"} {
		if strings.Contains(string(out), k) {
			t.Fatalf("%s should be omitted when empty: %s", k, out)
		}
	}
	b, _ := json.Marshal(TaskAttempt{SessionID: "s", TmuxSession: "t", EndReason: "stalled", EndDetail: "d"})
	var back TaskAttempt
	if err := json.Unmarshal(b, &back); err != nil || back.SessionID != "s" || back.TmuxSession != "t" || back.EndReason != "stalled" || back.EndDetail != "d" {
		t.Fatalf("round trip failed: %+v %v", back, err)
	}
}

func TestAttemptSessionRecord_RenderedInEscalationTextAndView(t *testing.T) {
	a := TaskAttempt{Number: 1, Outcome: "failed", SessionID: "sess-9", TmuxSession: "dw-9", EndReason: attemptEndStalled, EndDetail: "no output 10m"}
	text := formatEscalationAttemptFull(a)
	for _, want := range []string{"Session: sess-9 (tmux dw-9)", "Ended because: stalled: no output 10m"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(formatEscalationAttemptFull(TaskAttempt{Number: 1, Outcome: "failed"}), "Session:") {
		t.Fatal("old attempt must not render a session line")
	}
	e := buildTaskEntry(&Task{ID: "t", Attempts: []TaskAttempt{{Number: 1}, a}})
	if e.LastAttemptSessionID != "sess-9" || e.LastAttemptTmuxSession != "dw-9" || e.LastAttemptEndReason != attemptEndStalled || e.LastAttemptEndDetail != "no output 10m" {
		t.Fatalf("view = %+v", e)
	}
}

func TestAttemptSessionRecord_AdoptedRun(t *testing.T) {
	a := recordOne(t, nil, attemptRun{SessionID: "s1", Adopted: true}, attemptFailure{Outcome: attemptOutcomeFailed, EndReason: attemptEndVerifierFailed})
	if a.EndReason != attemptEndAdoptedThenFailed || a.SessionID != "s1" {
		t.Fatalf("got %q/%q", a.EndReason, a.SessionID)
	}
	a = recordOne(t, nil, attemptRun{SessionID: "s1", Adopted: true}, attemptFailure{Outcome: attemptOutcomeCancelled, EndReason: attemptEndDaemonRestart})
	if a.EndReason != attemptEndDaemonRestart {
		t.Fatalf("daemon_restart must keep its name, got %q", a.EndReason)
	}
}

func TestAttemptSessionRecord_TmuxFromLister(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	m.SetSessionListerFn(func() []SessionInfo {
		return []SessionInfo{{ID: "other", TmuxSession: "dw-other"}, {ID: "s1", TmuxSession: "dw-s1"}}
	})
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := &prd.Story[0].Tasks[0]
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{SessionID: "s1"}, attemptFailure{Outcome: attemptOutcomeFailed})
	if got := task.Attempts[0].TmuxSession; got != "dw-s1" {
		t.Fatalf("TmuxSession = %q, want dw-s1", got)
	}
}

func TestAttemptSessionRecord_InterruptedAttempt(t *testing.T) {
	// Daemon restart: executor stopped, PRD and task still active.
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := &prd.Story[0].Tasks[0]
	run := attemptRun{SessionID: "s1", AttemptsBefore: 0}
	m.recordInterruptedAttempt(prd, task, run)
	if len(task.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(task.Attempts))
	}
	a := task.Attempts[0]
	if a.EndReason != attemptEndDaemonRestart || a.Outcome != attemptOutcomeCancelled || a.SessionID != "s1" {
		t.Fatalf("got %+v", a)
	}
	if task.EscalationFailures != 0 {
		t.Fatal("an interrupted attempt must not count toward the escalation budget")
	}
	// Already recorded: no second attempt.
	m.recordInterruptedAttempt(prd, task, run)
	if len(task.Attempts) != 1 {
		t.Fatalf("recorded twice: %d", len(task.Attempts))
	}
	// No session started: nothing to close.
	m2, _ := newFailureEscalationFixture(t, t.TempDir())
	prd2 := newFailureEscalationPRD(t, m2, t.TempDir(), nil)
	t2 := &prd2.Story[0].Tasks[0]
	m2.recordInterruptedAttempt(prd2, t2, attemptRun{})
	if len(t2.Attempts) != 0 {
		t.Fatal("no attempt expected when no session started")
	}
}

func TestAttemptSessionRecord_OperatorCancel(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := &prd.Story[0].Tasks[0]
	prd.Status = PRDCancelled
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	m.recordInterruptedAttempt(prd, task, attemptRun{SessionID: "s1"})
	stored, _ := m.store.GetPRD(prd.ID)
	st, _ := findTaskInPRD(stored, task.ID)
	if len(st.Attempts) != 1 || st.Attempts[0].EndReason != attemptEndCancelledByOperator {
		t.Fatalf("attempts = %+v", st.Attempts)
	}
}
