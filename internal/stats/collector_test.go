package stats

import (
	"testing"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(t.TempDir())
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
}

func TestCollector_collect(t *testing.T) {
	c := NewCollector(t.TempDir())
	// collect is unexported but we can test via SetOnCollect
	var stats SystemStats
	c.SetOnCollect(func(s SystemStats) { stats = s })
	c.collect()
	if stats.MemTotal == 0 {
		t.Error("expected MemTotal > 0")
	}
}

func TestCollector_SetSessionCountFunc(t *testing.T) {
	c := NewCollector(t.TempDir())
	c.SetSessionCountFunc(func() (int, int) { return 3, 10 })
	var stats SystemStats
	c.SetOnCollect(func(s SystemStats) { stats = s })
	c.collect()
	if stats.ActiveSessions != 3 {
		t.Errorf("expected 3 active, got %d", stats.ActiveSessions)
	}
	if stats.TotalSessions != 10 {
		t.Errorf("expected 10 total, got %d", stats.TotalSessions)
	}
}

func TestCollector_SetRTKFunc(t *testing.T) {
	c := NewCollector(t.TempDir())
	c.SetRTKFunc(func(s *SystemStats) {
		s.RTKInstalled = true
		s.RTKVersion = "test-1.0"
	})
	var stats SystemStats
	c.SetOnCollect(func(s SystemStats) { stats = s })
	c.collect()
	if !stats.RTKInstalled {
		t.Error("expected RTKInstalled true")
	}
	if stats.RTKVersion != "test-1.0" {
		t.Errorf("expected 'test-1.0', got %q", stats.RTKVersion)
	}
}

func TestCollector_SetMemoryStatsFunc(t *testing.T) {
	c := NewCollector(t.TempDir())
	c.SetMemoryStatsFunc(func(s *SystemStats) {
		s.MemoryEnabled = true
		s.MemoryTotalCount = 42
	})
	var stats SystemStats
	c.SetOnCollect(func(s SystemStats) { stats = s })
	c.collect()
	if !stats.MemoryEnabled {
		t.Error("expected MemoryEnabled true")
	}
	if stats.MemoryTotalCount != 42 {
		t.Errorf("expected 42, got %d", stats.MemoryTotalCount)
	}
}

func TestChannelCounters(t *testing.T) {
	cc := &ChannelCounters{}
	cc.RecordSent(100)
	cc.RecordRecv(50)
	s := cc.Snapshot()
	if s.MsgSent != 1 {
		t.Errorf("expected 1 sent, got %d", s.MsgSent)
	}
	if s.MsgRecv != 1 {
		t.Errorf("expected 1 recv, got %d", s.MsgRecv)
	}
	if s.BytesOut != 100 {
		t.Errorf("expected 100 bytes out, got %d", s.BytesOut)
	}
}

func TestChannelCounters_RecordError(t *testing.T) {
	cc := &ChannelCounters{}
	cc.RecordError()
	cc.RecordError()
	s := cc.Snapshot()
	if s.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", s.Errors)
	}
}

func TestChannelTracker(t *testing.T) {
	tracker := NewChannelTracker()
	if tracker == nil {
		t.Fatal("expected non-nil")
	}

	ch := tracker.Get("signal")
	ch.RecordSent(100)
	ch.RecordRecv(50)

	ch2 := tracker.Get("telegram")
	ch2.RecordSent(200)

	snap := tracker.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(snap))
	}
	if snap["signal"].MsgSent != 1 {
		t.Errorf("expected signal 1 sent, got %d", snap["signal"].MsgSent)
	}
	if snap["telegram"].BytesOut != 200 {
		t.Errorf("expected telegram 200 bytes out, got %d", snap["telegram"].BytesOut)
	}
}

func TestCollector_SetOrphanDetectFunc(t *testing.T) {
	c := NewCollector(t.TempDir())
	c.SetOrphanDetectFunc(func() (int, []string) { return 2, []string{"cs-old1", "cs-old2"} })
	var stats SystemStats
	c.SetOnCollect(func(s SystemStats) { stats = s })
	c.collect()
	if len(stats.OrphanedTmux) != 2 {
		t.Errorf("expected 2 orphans, got %d", len(stats.OrphanedTmux))
	}
}
