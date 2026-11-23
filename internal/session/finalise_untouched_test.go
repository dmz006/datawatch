package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedUntouchedRecord stores rec (filling in a host, output log and an old
// updated-at when the caller left them unset) and returns its full ID and the
// stored record's JSON, which a test compares after the finaliser has run.
func seedUntouchedRecord(t *testing.T, mgr *Manager, rec Session) (string, []byte) {
	t.Helper()
	if rec.FullID == "" {
		rec.FullID = "testhost-" + rec.ID
	}
	if rec.Hostname == "" {
		rec.Hostname = "testhost"
	}
	if rec.LogFile == "" {
		dir := t.TempDir()
		rec.LogFile = filepath.Join(dir, "output.log")
		if err := os.WriteFile(rec.LogFile, []byte("output without the marker\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rec.ProjectDir = dir
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	}
	if err := mgr.SaveSession(&rec); err != nil {
		t.Fatal(err)
	}
	return rec.FullID, storedJSON(t, mgr, rec.FullID)
}

func storedJSON(t *testing.T, mgr *Manager, full string) []byte {
	t.Helper()
	got, ok := mgr.store.Get(full)
	if !ok {
		t.Fatalf("record %s missing", full)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// assertUntouched runs one finaliser pass and checks nothing was finalised and
// the stored record is byte-for-byte what it was before, with no state-change
// notification sent.
func assertUntouched(t *testing.T, mgr *Manager, full string, before []byte) FinaliseResult {
	t.Helper()
	pre, _ := mgr.store.Get(full)
	state, updated := pre.State, pre.UpdatedAt
	var ended *time.Time
	if pre.EndedAt != nil {
		e := *pre.EndedAt
		ended = &e
	}
	note := pre.EndNote

	var notified int
	mgr.onStateChange = func(*Session, State) { notified++ }
	res := mgr.FinaliseGoneSessions(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))

	if len(res.Finalised) != 0 {
		t.Fatalf("Finalised = %v, want none", res.Finalised)
	}
	if notified != 0 {
		t.Fatalf("state-change handler called %d times, want 0", notified)
	}
	got, ok := mgr.store.Get(full)
	if !ok {
		t.Fatalf("record %s missing after finalise", full)
	}
	if got.State != state {
		t.Fatalf("state = %q, want %q", got.State, state)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("updated_at = %v, want %v", got.UpdatedAt, updated)
	}
	switch {
	case ended == nil && got.EndedAt != nil:
		t.Fatalf("ended_at = %v, want unset", got.EndedAt)
	case ended != nil && (got.EndedAt == nil || !got.EndedAt.Equal(*ended)):
		t.Fatalf("ended_at = %v, want %v", got.EndedAt, ended)
	}
	if got.EndNote != note {
		t.Fatalf("end_note = %q, want %q", got.EndNote, note)
	}
	if after := storedJSON(t, mgr, full); string(after) != string(before) {
		t.Fatalf("stored record changed:\nbefore: %s\nafter:  %s", before, after)
	}
	return res
}

func TestFinaliseLeavesLiveTerminalSessionUntouched(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	if err := fake.NewSessionWithSize("cs-lt01", 80, 24); err != nil {
		t.Fatal(err)
	}
	full, before := seedUntouchedRecord(t, mgr, Session{
		ID: "lt01", TmuxSession: "cs-lt01", State: StateRunning,
	})
	res := assertUntouched(t, mgr, full, before)
	if !res.TerminalListOK {
		t.Fatal("terminal listing must succeed")
	}
	if !fake.SessionExists("cs-lt01") {
		t.Fatal("the terminal session must still exist")
	}
}

func TestFinaliseLeavesChatModeRunnerSessionUntouched(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	full, before := seedUntouchedRecord(t, mgr, Session{
		ID: "cm01", OutputMode: "chat", State: StateRunning,
	})
	mgr.RegisterRunner(full)
	assertUntouched(t, mgr, full, before)
	if !mgr.HasRunner(full) {
		t.Fatal("the runner registration must be left in place")
	}
}

func TestFinaliseLeavesSessionWithLaunchInFlightUntouched(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	rec := Session{ID: "lf01", TmuxSession: "cs-lf01", State: StateRunning}
	full, before := seedUntouchedRecord(t, mgr, rec)
	stored, _ := mgr.store.Get(full)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.registerLaunch(stored, cancel); err != nil {
		t.Fatal(err)
	}
	defer mgr.deregisterLaunch(full)
	assertUntouched(t, mgr, full, before)
	if mgr.launchCount() != 1 {
		t.Fatalf("launchCount = %d, want 1", mgr.launchCount())
	}
}

func TestFinaliseLeavesForeignHostSessionUntouched(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	full, before := seedUntouchedRecord(t, mgr, Session{
		ID: "fh01", FullID: "otherhost-fh01", Hostname: "otherhost",
		TmuxSession: "cs-fh01", State: StateRunning,
	})
	assertUntouched(t, mgr, full, before)
}

func TestFinaliseSkipsWhenTerminalListingFails(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	// A gone record that would be finalised if the listing worked.
	full, before := seedUntouchedRecord(t, mgr, Session{
		ID: "tl01", TmuxSession: "cs-tl01", State: StateRunning,
	})
	fake.ListSessionsErr = errors.New("tmux: unexpected listing failure")
	res := assertUntouched(t, mgr, full, before)
	if res.TerminalListOK {
		t.Fatal("TerminalListOK = true, want false when the listing fails")
	}
	if res.Skipped != 0 {
		t.Fatalf("Skipped = %d, want 0 (nothing was examined)", res.Skipped)
	}
}

func TestFinaliseLeavesFinalStatesUntouched(t *testing.T) {
	ended := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	for _, state := range []State{StateComplete, StateFailed, StateKilled} {
		t.Run(string(state), func(t *testing.T) {
			mgr, _ := newTestManagerWithFake(t)
			e := ended
			full, before := seedUntouchedRecord(t, mgr, Session{
				ID: "fs01", TmuxSession: "cs-fs01", State: state,
				EndedAt: &e, EndNote: "ended earlier",
				UpdatedAt: time.Date(2026, 11, 10, 9, 31, 0, 0, time.UTC),
			})
			assertUntouched(t, mgr, full, before)
		})
	}
}

func TestFinaliseLeavesDaemonOwnedVirtualSessionUntouched(t *testing.T) {
	cases := map[string]Session{
		"virtual backend family": {ID: "dv01", BackendFamily: "ollama-virtual"},
		"agent bound":            {ID: "dv02", AgentID: "agent-1"},
	}
	for name, rec := range cases {
		t.Run(name, func(t *testing.T) {
			mgr, _ := newTestManagerWithFake(t)
			rec.State = StateRunning
			full, before := seedUntouchedRecord(t, mgr, rec)
			assertUntouched(t, mgr, full, before)
		})
	}
}
