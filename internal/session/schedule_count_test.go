// BL116 — CountForSession test.

package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleStore_CountForSession(t *testing.T) {
	dir := t.TempDir()
	s, err := NewScheduleStore(filepath.Join(dir, "sched.json"))
	if err != nil {
		t.Fatal(err)
	}

	if got := s.CountForSession("nope"); got != 0 {
		t.Errorf("empty store count=%d want 0", got)
	}

	now := time.Now().Add(time.Hour)
	_, _ = s.Add("sess-a", "yes", now, "")
	_, _ = s.Add("sess-a", "no", now, "")
	_, _ = s.Add("sess-b", "go", now, "")

	if got := s.CountForSession("sess-a"); got != 2 {
		t.Errorf("sess-a count=%d want 2", got)
	}
	if got := s.CountForSession("sess-b"); got != 1 {
		t.Errorf("sess-b count=%d want 1", got)
	}
	if got := s.CountForSession("sess-z"); got != 0 {
		t.Errorf("unknown session count=%d want 0", got)
	}
}

func TestScheduleStore_CountForSession_IgnoresCancelled(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewScheduleStore(filepath.Join(dir, "sched.json"))
	now := time.Now().Add(time.Hour)
	a, _ := s.Add("sess-a", "yes", now, "")
	_, _ = s.Add("sess-a", "still-pending", now, "")

	// Mark the first one cancelled (Cancel exists on ScheduleStore).
	_ = s.Cancel(a.ID)

	if got := s.CountForSession("sess-a"); got != 1 {
		t.Errorf("after cancel, count=%d want 1", got)
	}
}
