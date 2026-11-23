package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedGoneRecord stores a record in the given state with no terminal and an
// output log holding logText, and returns the record's full ID and log path.
func seedGoneRecord(t *testing.T, mgr *Manager, id string, state State, logText string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "output.log")
	if err := os.WriteFile(logPath, []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	alive := true
	full := "testhost-" + id
	if err := mgr.SaveSession(&Session{
		ID: id, FullID: full, Hostname: "testhost", TmuxSession: "cs-" + id,
		State: state, LogFile: logPath, ProjectDir: dir, ClaudeAlive: &alive,
		UpdatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return full, logPath
}

func finaliseAndGet(t *testing.T, mgr *Manager, full string, now time.Time) (*Session, FinaliseResult) {
	t.Helper()
	res := mgr.FinaliseGoneSessions(now)
	if !res.TerminalListOK {
		t.Fatal("terminal listing must succeed against the fake")
	}
	got, ok := mgr.store.Get(full)
	if !ok {
		t.Fatalf("record %s missing after finalise", full)
	}
	return got, res
}

func assertFinalisedKilled(t *testing.T, state State) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	full, _ := seedGoneRecord(t, mgr, "fk01", state, "some output without the marker\n")
	got, res := finaliseAndGet(t, mgr, full, time.Now())
	if got.State != StateKilled {
		t.Fatalf("state = %q, want %q", got.State, StateKilled)
	}
	if len(res.Finalised) != 1 || res.Finalised[0] != full {
		t.Fatalf("Finalised = %v, want [%s]", res.Finalised, full)
	}
}

func TestFinaliseRunningWithNoTerminalBecomesKilled(t *testing.T) {
	assertFinalisedKilled(t, StateRunning)
}

func TestFinaliseWaitingInputWithNoTerminalBecomesKilled(t *testing.T) {
	assertFinalisedKilled(t, StateWaitingInput)
}

func TestFinaliseStalledWithNoTerminalBecomesKilled(t *testing.T) {
	assertFinalisedKilled(t, StateStalled)
}

func TestFinaliseRateLimitedWithNoTerminalBecomesKilled(t *testing.T) {
	assertFinalisedKilled(t, StateRateLimited)
}

func TestFinaliseCompleteWhenOutputEndsWithMarker(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	full, _ := seedGoneRecord(t, mgr, "fc01", StateRunning,
		"work done\n\x1b[32mDATAWATCH_COMPLETE: all finished\x1b[0m\n")
	got, _ := finaliseAndGet(t, mgr, full, time.Now())
	if got.State != StateComplete {
		t.Fatalf("state = %q, want %q", got.State, StateComplete)
	}
}

func TestFinaliseSetsEndTimeAndNote(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	full, _ := seedGoneRecord(t, mgr, "fe01", StateRunning, "output\n")
	var gotOld State
	var calls int
	mgr.onStateChange = func(_ *Session, old State) { gotOld = old; calls++ }
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	got, res := finaliseAndGet(t, mgr, full, now)
	if got.EndedAt == nil || !got.EndedAt.Equal(now) {
		t.Fatalf("EndedAt = %v, want %v", got.EndedAt, now)
	}
	if got.EndNote != GoneEndNote {
		t.Fatalf("EndNote = %q, want %q", got.EndNote, GoneEndNote)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}
	if got.ClaudeAlive != nil {
		t.Fatalf("ClaudeAlive = %v, want nil", *got.ClaudeAlive)
	}
	if !res.At.Equal(now) {
		t.Fatalf("result At = %v, want %v", res.At, now)
	}
	if calls != 1 || gotOld != StateRunning {
		t.Fatalf("state-change handler calls=%d old=%q, want 1 call with %q", calls, gotOld, StateRunning)
	}
}

func TestFinaliseKeepsRecordAndOutput(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	const text = "line one\nline two\n"
	full, logPath := seedGoneRecord(t, mgr, "fp01", StateRunning, text)
	dir := filepath.Dir(logPath)
	got, res := finaliseAndGet(t, mgr, full, time.Now())
	if res.Folders != 1 {
		t.Fatalf("Folders = %d, want 1", res.Folders)
	}
	if got.LogFile != logPath || got.ProjectDir != dir {
		t.Fatalf("record paths changed: log=%q dir=%q", got.LogFile, got.ProjectDir)
	}
	data, err := os.ReadFile(logPath)
	if err != nil || string(data) != text {
		t.Fatalf("output log changed: %q, %v", data, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("folder removed: %v", err)
	}
	// A second pass leaves the now-final record alone.
	again := mgr.FinaliseGoneSessions(time.Now())
	if len(again.Finalised) != 0 || again.Skipped != 1 {
		t.Fatalf("second pass = %+v, want nothing finalised and 1 skipped", again)
	}
}
