package autonomous

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVerifierRunCapStatsView_ZeroState(t *testing.T) {
	clk := &verifierFakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	v := NewVerifierStats(clk.now).Snapshot()
	if v.RunCapSeconds != 0 {
		t.Fatalf("run_cap_seconds = %d, want 0", v.RunCapSeconds)
	}
	if v.RunCapEnds != 0 {
		t.Fatalf("run_cap_ends = %d, want 0", v.RunCapEnds)
	}
}

func TestVerifierRunCapStatsView_SetRunCapSeconds(t *testing.T) {
	clk := &verifierFakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	s := NewVerifierStats(clk.now)
	s.SetRunCapSeconds(2700)
	if got := s.Snapshot().RunCapSeconds; got != 2700 {
		t.Fatalf("run_cap_seconds = %d, want 2700", got)
	}
	// The cap is configuration: recording a run must not clear it.
	s.Record(time.Second, time.Minute, VerifierCatPass)
	if got := s.Snapshot().RunCapSeconds; got != 2700 {
		t.Fatalf("after Record run_cap_seconds = %d, want 2700", got)
	}
	s.SetRunCapSeconds(-5)
	if got := s.Snapshot().RunCapSeconds; got != 0 {
		t.Fatalf("negative cap run_cap_seconds = %d, want 0", got)
	}
}

func TestVerifierRunCapStatsView_JSONKeys(t *testing.T) {
	clk := &verifierFakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	s := NewVerifierStats(clk.now)
	s.SetRunCapSeconds(2700)
	raw, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := m["run_cap_seconds"]; !ok || got != float64(2700) {
		t.Fatalf("run_cap_seconds = %v (present=%v), want 2700", got, ok)
	}
	if got, ok := m["run_cap_ends"]; !ok || got != float64(0) {
		t.Fatalf("run_cap_ends = %v (present=%v), want 0", got, ok)
	}
}
