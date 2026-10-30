package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Failure paths of the supervisor watch store and the small adapters
// beside it: a corrupt file refuses to open, a persist failure rolls the
// in-memory change back, and a missing key is an error.

func swStoreWithDir(t *testing.T) (*SupervisorWatchStore, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewSupervisorWatchStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func TestSupervisorWatchStoreCorruptFilesRefuseToOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SupervisorWatchFileName), []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupervisorWatchStore(dir); err == nil {
		t.Error("a corrupt findings file must refuse to open")
	}

	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SupervisorWatchComparisonFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupervisorWatchStore(dir); err == nil {
		t.Error("a corrupt comparison history must refuse to open")
	}
}

func TestSupervisorWatchStoreMissingKeys(t *testing.T) {
	st, _ := swStoreWithDir(t)
	if f, ok := st.Get("nope"); ok || f != nil {
		t.Errorf("Get of a missing key = %v, %v", f, ok)
	}
	if cloneSupervisorWatchFinding(nil) != nil {
		t.Error("cloning nil stays nil")
	}
	if _, err := st.Resolve("nope"); err == nil {
		t.Error("resolving a missing key must be an error")
	}
	if err := st.Delete("nope"); err == nil {
		t.Error("deleting a missing key must be an error")
	}
	if _, err := st.Upsert(&SupervisorWatchFinding{}); err == nil {
		t.Error("a finding with no kind or subject must be refused")
	}

	f := swFinding(SupervisorWatchTaskFailed, "p1", "x")
	if _, err := st.Upsert(&f); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(f.Key()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := st.Get(f.Key()); ok {
		t.Error("a deleted finding must be gone")
	}
}

func TestSupervisorWatchStorePersistFailureRollsBack(t *testing.T) {
	st, dir := swStoreWithDir(t)
	old := swFinding(SupervisorWatchTaskFailed, "p1", "first")
	if _, err := st.Upsert(&old); err != nil {
		t.Fatal(err)
	}
	// the data directory disappears: every write now fails
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	changed := swFinding(SupervisorWatchTaskFailed, "p1", "second")
	if _, err := st.Upsert(&changed); err == nil {
		t.Error("an upsert that cannot persist must be an error")
	}
	if got, _ := st.Get(old.Key()); got == nil || got.Text != "first" {
		t.Errorf("a failed upsert of an existing finding must roll back, got %+v", got)
	}

	fresh := swFinding(SupervisorWatchCIRed, "9", "new")
	if _, err := st.Upsert(&fresh); err == nil {
		t.Error("a new finding that cannot persist must be an error")
	}
	if _, ok := st.Get(fresh.Key()); ok {
		t.Error("a failed upsert of a new finding must not leave it stored")
	}

	if _, err := st.Resolve(old.Key()); err == nil {
		t.Error("a resolve that cannot persist must be an error")
	}
	if got, _ := st.Get(old.Key()); got == nil || got.Resolved {
		t.Errorf("a failed resolve must roll back, got %+v", got)
	}

	if err := st.Delete(old.Key()); err == nil {
		t.Error("a delete that cannot persist must be an error")
	}
	if _, ok := st.Get(old.Key()); !ok {
		t.Error("a failed delete must restore the finding")
	}

	if _, err := st.RecordComparison(SupervisorWatchComparison{At: time.Now(), Agreed: true}); err == nil {
		t.Error("a comparison that cannot persist must be an error")
	}
	if n := len(st.ListComparisons()); n != 0 {
		t.Errorf("a failed comparison must roll back, history has %d", n)
	}
	if st.DayAgreement().FullAgreementDays() != 0 {
		t.Error("a failed comparison must not count toward a day")
	}
}

func TestSupervisorWatchQueuedEligibleByLane(t *testing.T) {
	q := LaneFillQueued{GPU: 3, Claude: 5}
	if got := supervisorWatchQueuedEligible(LaneTypeGPU, q); got != 3 {
		t.Errorf("gpu = %d", got)
	}
	if got := supervisorWatchQueuedEligible(LaneTypeClaude, q); got != 5 {
		t.Errorf("claude = %d", got)
	}
	if got := supervisorWatchQueuedEligible(LaneType("other"), q); got != 0 {
		t.Errorf("an unknown lane type reads zero, got %d", got)
	}
}

func TestSessionSupervisorWatchSessionLoggerEdges(t *testing.T) {
	sessions := map[string]*session.Session{
		"s-log":   {LogFile: " logs/s-log.log "},
		"s-nolog": {LogFile: "  "},
		"s-nil":   nil,
	}
	a := SessionSupervisorWatchSessionLogger{GetSession: func(id string) (*session.Session, bool) {
		s, ok := sessions[id]
		return s, ok
	}}
	if p, ok := a.SessionLogPath(" s-log "); !ok || p != "logs/s-log.log" {
		t.Errorf("known session = %q, %v", p, ok)
	}
	for _, id := range []string{"", "  ", "unknown", "s-nolog", "s-nil"} {
		if p, ok := a.SessionLogPath(id); ok || p != "" {
			t.Errorf("session %q must read as no known log, got %q %v", id, p, ok)
		}
	}
	if _, ok := (SessionSupervisorWatchSessionLogger{}).SessionLogPath("s-log"); ok {
		t.Error("an unwired lookup reads as no known log")
	}
}
