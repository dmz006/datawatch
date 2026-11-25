// Every path that moves a session to complete, failed or killed leaves the end
// time and the exit note on the record, and every path that brings a session
// back clears them.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run 'EndTimeAndNote|ClearsEndRecord'

package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

// wantEndRecord fails unless the stored record is in state want and says when
// and why it ended.
func wantEndRecord(t *testing.T, mgr *Manager, fullID string, want State) {
	t.Helper()
	cur, ok := mgr.GetSession(fullID)
	if !ok {
		t.Fatalf("session %s missing", fullID)
	}
	if cur.State != want {
		t.Fatalf("state = %s, want %s", cur.State, want)
	}
	if cur.EndedAt == nil || cur.EndedAt.IsZero() {
		t.Errorf("%s record has no end time", want)
	}
	if strings.TrimSpace(cur.EndNote) == "" {
		t.Errorf("%s record has no exit note", want)
	}
}

// resumeOne runs a boot step on a manager and stops its monitors afterwards.
func resumeOne(t *testing.T, mgr *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	mgr.ResumeMonitors(ctx)
	t.Cleanup(func() {
		cancel()
		mgr.WaitBackground()
	})
}

func localEndSession(t *testing.T, mgr *Manager, id string, state State) *Session {
	t.Helper()
	s := endTestSession(t, mgr, id, state)
	s.Hostname = "testhost"
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReapFinishedOneShots_RecordsEndTimeAndNote(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee01", StateRunning)
	s.OneShot = true
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	tdrSetPane(fake, s.TmuxSession, "worker output\n"+oneShotCompletionMarker+" job done\n")
	if got := mgr.ReapFinishedOneShots(); len(got) != 1 {
		t.Fatalf("reaped %v, want the one finished session", got)
	}
	wantEndRecord(t, mgr, s.FullID, StateComplete)
}

func TestResumeMonitorsGoneTerminal_RecordsEndTimeAndNote(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee02", StateRunning)
	resumeOne(t, mgr)
	wantEndRecord(t, mgr, s.FullID, StateFailed)
}

func TestReconnectBackendsGoneTerminal_RecordsEndTimeAndNote(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee03", StateRunning)
	s.TrackingDir = t.TempDir()
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	mgr.ReconnectBackends(func(*Session, *BackendState) {})
	wantEndRecord(t, mgr, s.FullID, StateComplete)
}

func TestDrainLaunches_RecordsEndTimeAndNote(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee04", StateRunning)
	if err := mgr.registerLaunch(s, func() { mgr.deregisterLaunch(s.FullID) }); err != nil {
		t.Fatal(err)
	}
	// The cancel runs under the manager lock in DrainLaunches, so it must not
	// take it again: replace it with one that does nothing.
	mgr.mu.Lock()
	mgr.launching[s.FullID] = func() {}
	mgr.mu.Unlock()
	if got := mgr.DrainLaunches(time.Millisecond); len(got) != 1 {
		t.Fatalf("drained %v, want the stuck launch", got)
	}
	wantEndRecord(t, mgr, s.FullID, StateKilled)
}

func TestSetStateTerminal_RecordsEndTimeAndNote(t *testing.T) {
	for i, state := range []State{StateComplete, StateFailed, StateKilled} {
		mgr, _ := newTestManagerWithFake(t)
		s := localEndSession(t, mgr, "ee1"+string(rune('0'+i)), StateRunning)
		if err := mgr.SetState(s.FullID, state); err != nil {
			t.Fatal(err)
		}
		wantEndRecord(t, mgr, s.FullID, state)
	}
}

// ended gives a record the stamp of an earlier end.
func ended(t *testing.T, mgr *Manager, s *Session) {
	t.Helper()
	at := time.Now().Add(-time.Hour)
	s.EndedAt, s.EndNote = &at, "killed by an earlier end"
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
}

func wantNoEndRecord(t *testing.T, mgr *Manager, fullID string) {
	t.Helper()
	cur, ok := mgr.GetSession(fullID)
	if !ok {
		t.Fatalf("session %s missing", fullID)
	}
	if cur.EndedAt != nil || cur.EndNote != "" {
		t.Errorf("a %s session still carries an end record: ended_at=%v note=%q", cur.State, cur.EndedAt, cur.EndNote)
	}
}

func TestSetStateBackToRunning_ClearsEndRecord(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee20", StateFailed)
	ended(t, mgr, s)
	if err := mgr.SetState(s.FullID, StateRunning); err != nil {
		t.Fatal(err)
	}
	wantNoEndRecord(t, mgr, s.FullID)
}

func TestResumeMonitorsRecovery_ClearsEndRecord(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	s := localEndSession(t, mgr, "ee21", StateFailed)
	ended(t, mgr, s)
	tdrSetPane(fake, s.TmuxSession, tdIdlePane)
	resumeOne(t, mgr)
	wantNoEndRecord(t, mgr, s.FullID)
}

func TestRestart_ClearsEndRecordAndTheNextEndIsRecorded(t *testing.T) {
	isolateMultiplexerEnv(t)
	mgr, fake := newPromptFamilyTestManager(t)
	started, err := mgr.Start(context.Background(), "one-shot pi job", "", t.TempDir(), &StartOptions{
		Backend:  "pi-prompt",
		LaunchFn: stubPromptLaunch(t, fake),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := mgr.EndSession(started.FullID, EndReason{Caller: "test", Reason: "first end"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		mgr.WaitBackground()
	})
	if _, err := mgr.Restart(ctx, started.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	wantNoEndRecord(t, mgr, started.FullID)
	if err := mgr.EndSession(started.FullID, EndReason{Caller: "test", Reason: "second end"}); err != nil {
		t.Fatal(err)
	}
	cur, _ := mgr.GetSession(started.FullID)
	if !strings.Contains(cur.EndNote, "second end") {
		t.Errorf("exit note after the second end = %q, want the second end's reason", cur.EndNote)
	}
}
