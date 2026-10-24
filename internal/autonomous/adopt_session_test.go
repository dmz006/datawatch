package autonomous

import (
	"context"
	"errors"
	"testing"
)

func TestResetInProgressAdoptsSurvivingSession(t *testing.T) {
	const doneOut = "work\nDATAWATCH_COMPLETE: did the thing\n"
	cases := []struct {
		name       string
		sessionID  string
		alive      bool
		output     string
		outputOK   bool
		wantStatus TaskStatus
		wantAdopt  bool
		wantSess   string
		wantKilled bool
	}{
		{"live session adopted", "sess-live", true, "", false, TaskInProgress, true, "sess-live", false},
		{"dead no marker reset", "sess-dead", false, "still working\n", true, TaskPending, false, "", true},
		{"dead no record reset", "sess-gone", false, "", false, TaskPending, false, "", true},
		{"empty session reset", "", false, "", false, TaskPending, false, "", false},
		{"dead with marker verifying", "sess-done", false, doneOut, true, TaskVerifying, false, "sess-done", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := verifyingBootManager(t, []Story{
				{Title: "story", Tasks: []Task{{Title: "task", Status: TaskInProgress, SessionID: tc.sessionID}}},
			})
			var killed []string
			m.SetSessionKillerFn(func(id, _, _ string) error { killed = append(killed, id); return nil })
			m.SetSessionAliveFn(func(string) bool { return tc.alive })
			m.SetSessionOutputFn(func(string) (string, bool) { return tc.output, tc.outputOK })
			m.resetInProgressTasksForResume(prd)

			got, _ := m.Store().GetPRD(prd.ID)
			task := got.Story[0].Tasks[0]
			if task.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", task.Status, tc.wantStatus)
			}
			if task.AdoptedSession != tc.wantAdopt {
				t.Fatalf("AdoptedSession = %v, want %v", task.AdoptedSession, tc.wantAdopt)
			}
			if task.SessionID != tc.wantSess {
				t.Fatalf("SessionID = %q, want %q", task.SessionID, tc.wantSess)
			}
			if (len(killed) > 0) != tc.wantKilled {
				t.Fatalf("killed = %v, want killed=%v", killed, tc.wantKilled)
			}
			if got.Status != PRDRunning {
				t.Fatalf("prd status = %q, want running", got.Status)
			}
		})
	}
}

func TestWaitForAdoptedSession(t *testing.T) {
	boom := errors.New("boom")
	if err := waitForAdoptedSession(context.Background(), func(context.Context, string) error { return boom }, nil, "s"); !errors.Is(err, boom) {
		t.Fatalf("wait hook error not propagated: %v", err)
	}
	if err := waitForAdoptedSession(context.Background(), nil, func(string) bool { return false }, "s"); err != nil {
		t.Fatalf("dead session fallback: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForAdoptedSession(ctx, nil, func(string) bool { return true }, "s"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: %v", err)
	}
}
