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
