package git

import (
	"testing"
	"time"
)

func TestHostOpCountersWindowAndLastSuccess(t *testing.T) {
	resetHostOpCounters()
	old := hostOpNow
	defer func() { hostOpNow = old; resetHostOpCounters() }()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	hostOpNow = func() time.Time { return now }

	if s := HostOpStatsSnapshot(); !s.LastSuccessAt.IsZero() || s.Failed != 0 {
		t.Fatalf("fresh snapshot not empty: %+v", s)
	}
	RecordHostOp(HostOpPush, true)
	now = now.Add(10 * time.Minute)
	RecordHostOp(HostOpPullRequest, true)
	RecordHostOp(HostOpCIRead, false)
	RecordHostOp(HostOpTag, true)
	okAt := now
	now = now.Add(time.Minute)
	RecordHostOp(HostOpPush, false)

	s := HostOpStatsSnapshot()
	if s.Operations[HostOpPush] != 2 || s.Operations[HostOpPullRequest] != 1 ||
		s.Operations[HostOpTag] != 1 || s.Operations[HostOpCIRead] != 1 || s.Operations[HostOpMerge] != 0 {
		t.Fatalf("operations = %v", s.Operations)
	}
	if s.Failed != 2 {
		t.Fatalf("failed = %d, want 2", s.Failed)
	}
	if !s.LastSuccessAt.Equal(okAt) {
		t.Fatalf("last success = %v, want %v", s.LastSuccessAt, okAt)
	}

	// Move past the window for the first push only.
	now = now.Add(HostOpWindow - 10*time.Minute + time.Second)
	s = HostOpStatsSnapshot()
	if s.Operations[HostOpPush] != 1 {
		t.Fatalf("push after expiry = %d, want 1", s.Operations[HostOpPush])
	}
	// Everything expires; the last success time is kept.
	now = now.Add(2 * HostOpWindow)
	s = HostOpStatsSnapshot()
	if s.Failed != 0 || s.Operations[HostOpTag] != 0 {
		t.Fatalf("expected empty window, got %+v", s)
	}
	if !s.LastSuccessAt.Equal(okAt) {
		t.Fatalf("last success lost: %v", s.LastSuccessAt)
	}
}
