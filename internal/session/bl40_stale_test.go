// BL40 — stale-task detection tests.

package session

import (
	"testing"
	"time"
)

func TestBL40_IsStale_NilSession(t *testing.T) {
	if IsStale(nil, time.Hour, time.Now()) {
		t.Error("nil session should never be stale")
	}
}

func TestBL40_IsStale_DisabledByZeroThreshold(t *testing.T) {
	sess := &Session{State: StateRunning, UpdatedAt: time.Now().Add(-24 * time.Hour)}
	if IsStale(sess, 0, time.Now()) {
		t.Error("threshold=0 should disable")
	}
}

func TestBL40_IsStale_NotRunning(t *testing.T) {
	sess := &Session{State: StateComplete, UpdatedAt: time.Now().Add(-24 * time.Hour)}
	if IsStale(sess, time.Minute, time.Now()) {
		t.Error("non-running sessions should never be stale")
	}
}

func TestBL40_IsStale_OldRunningHits(t *testing.T) {
	now := time.Now()
	sess := &Session{State: StateRunning, UpdatedAt: now.Add(-2 * time.Hour)}
	if !IsStale(sess, time.Hour, now) {
		t.Error("2h-old running session vs 1h threshold should be stale")
	}
}

func TestBL40_IsStale_FreshDoesNot(t *testing.T) {
	now := time.Now()
	sess := &Session{State: StateRunning, UpdatedAt: now.Add(-30 * time.Second)}
	if IsStale(sess, time.Hour, now) {
		t.Error("30s-old session vs 1h threshold should not be stale")
	}
}

func TestBL40_ListStale_FiltersHostAndState(t *testing.T) {
	now := time.Now()
	sessions := []*Session{
		{Hostname: "h1", State: StateRunning, FullID: "h1-aa", UpdatedAt: now.Add(-2 * time.Hour)},
		{Hostname: "h1", State: StateComplete, FullID: "h1-bb", UpdatedAt: now.Add(-2 * time.Hour)},
		{Hostname: "h2", State: StateRunning, FullID: "h2-cc", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	out := ListStale(sessions, "h1", time.Hour, now)
	if len(out) != 1 {
		t.Fatalf("want 1 stale on h1, got %d: %+v", len(out), out)
	}
	if out[0].FullID != "h1-aa" {
		t.Errorf("wrong session selected: %+v", out[0])
	}
	if out[0].StaleSeconds < 7000 { // 2h ≈ 7200s
		t.Errorf("StaleSeconds=%d, expected ~7200", out[0].StaleSeconds)
	}
}

func TestBL40_ListStale_NoHostFilter(t *testing.T) {
	now := time.Now()
	sessions := []*Session{
		{Hostname: "h1", State: StateRunning, FullID: "h1-aa", UpdatedAt: now.Add(-2 * time.Hour)},
		{Hostname: "h2", State: StateRunning, FullID: "h2-bb", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	out := ListStale(sessions, "", time.Hour, now)
	if len(out) != 2 {
		t.Errorf("want 2 (no host filter), got %d", len(out))
	}
}
