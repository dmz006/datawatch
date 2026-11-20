package session

import (
	"testing"
	"time"
)

func TestActiveFinishedSession(t *testing.T) {
	base := time.Now()
	s := Session{State: StateComplete, CreatedAt: base, UpdatedAt: base.Add(90 * time.Second)}
	if got := s.ActiveSeconds(base.Add(time.Hour)); got != 90 {
		t.Fatalf("active=%v want 90", got)
	}
}

func TestActiveRunningSession(t *testing.T) {
	base := time.Now()
	s := Session{State: StateRunning, CreatedAt: base}
	if got := s.ActiveSeconds(base.Add(30 * time.Second)); got != 30 {
		t.Fatalf("active=%v want 30", got)
	}
}

func TestActiveIdleSubtracted(t *testing.T) {
	base := time.Now()
	now := base
	old := activeClock
	activeClock = func() time.Time { return now }
	defer func() { activeClock = old }()

	s := &Session{State: StateRunning, CreatedAt: base}
	now = base.Add(10 * time.Second)
	s.State = StateWaitingInput
	s.trackIdle(activeClock())
	now = base.Add(40 * time.Second)
	// open idle stretch: 30s idle of 40s total
	if got := s.ActiveSeconds(now); got != 10 {
		t.Fatalf("open idle active=%v want 10", got)
	}
	s.State = StateRunning
	s.trackIdle(activeClock())
	now = base.Add(50 * time.Second)
	if got := s.ActiveSeconds(now); got != 20 {
		t.Fatalf("closed idle active=%v want 20", got)
	}
	s.State, s.UpdatedAt = StateComplete, now
	if got := s.ActiveSeconds(now.Add(time.Hour)); got != 20 {
		t.Fatalf("finished active=%v want 20", got)
	}
}
