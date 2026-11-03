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

func TestAttemptSessionRecord_SessionFromTaskAndRun(t *testing.T) {
	a := recordOne(t, func(tk *Task) { tk.SessionID = "sess-task" }, attemptRun{}, attemptFailure{Outcome: attemptOutcomeFailed})
	if a.SessionID != "sess-task" {
		t.Fatalf("SessionID = %q, want task session", a.SessionID)
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
		{"adopted", func(tk *Task) { tk.AdoptedSession = true }, attemptFailure{Outcome: attemptOutcomeFailed}, attemptEndAdoptedThenFailed, ""},
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
