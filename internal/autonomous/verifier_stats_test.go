package autonomous

import (
	"strings"
	"testing"
	"time"
)

type verifierFakeClock struct{ t time.Time }

func (c *verifierFakeClock) now() time.Time { return c.t }

func TestVerifierMetrics_AggregatesWithFakeClock(t *testing.T) {
	clk := &verifierFakeClock{t: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}
	s := NewVerifierStats(clk.now)
	s.Record(30*time.Second, 5*time.Minute, VerifierCatPass)
	s.Record(90*time.Second, 7*time.Minute, VerifierCatInfra)
	v := s.Snapshot()
	if v.Runs != 2 || v.WaitTotalSec != 120 || v.WaitAvgSec != 60 || v.WaitMaxSec != 90 {
		t.Fatalf("wait aggregates wrong: %+v", v)
	}
	if v.RunTotalSec != 720 || v.RunAvgSec != 360 || v.RunMaxSec != 420 {
		t.Fatalf("run aggregates wrong: %+v", v)
	}
	if !v.Since.Equal(clk.t) {
		t.Fatalf("since = %v", v.Since)
	}
}

func TestVerifierMetrics_CategoriesCountedSeparately(t *testing.T) {
	s := NewVerifierStats(nil)
	for _, c := range []string{VerifierCatPass, VerifierCatPass, VerifierCatContent, VerifierCatInfra,
		VerifierCatWatchdog, VerifierCatTimeout, VerifierCatCapacity, VerifierCatEscalated} {
		s.Record(time.Second, time.Second, c)
	}
	v := s.Snapshot()
	want := map[string]int{"pass": 2, "content": 1, "infra": 1, "watchdog": 1, "timeout": 1, "capacity": 1, "escalated": 1}
	for k, n := range want {
		if v.Categories[k] != n {
			t.Errorf("category %s = %d, want %d", k, v.Categories[k], n)
		}
	}
	if v.WatchdogEnds != 1 || v.Escalations != 1 {
		t.Fatalf("hook counters: %+v", v)
	}
	if got := VerifierCategoryFor(false, "infra", "verifier(session): verifier no-progress watchdog: x", false); got != VerifierCatWatchdog {
		t.Errorf("watchdog mapped to %s", got)
	}
	if got := VerifierCategoryFor(false, "", "bad diff", false); got != VerifierCatContent {
		t.Errorf("content mapped to %s", got)
	}
	if got := VerifierCategoryFor(true, "", "", true); got != VerifierCatEscalated {
		t.Errorf("escalated mapped to %s", got)
	}
}

func TestVerifierMetrics_ZeroStateRenders(t *testing.T) {
	v := NewVerifierStats(nil).Snapshot()
	if v.WaitAvgSec != 0 || v.RunAvgSec != 0 {
		t.Fatalf("zero state averages: %+v", v)
	}
	if got := FormatVerifierLine(v); !strings.Contains(got, "no runs yet") {
		t.Fatalf("zero line = %q", got)
	}
	out, err := FormatCapacityText([]byte(`{"pools":[],"verifier":{"runs":0,"categories":{}}}`))
	if err != nil || !strings.Contains(out, "verifiers: no runs yet") {
		t.Fatalf("zero text: %v %q", err, out)
	}
}

func TestVerifierMetrics_StatusTextLine(t *testing.T) {
	s := NewVerifierStats(nil)
	for i := 0; i < 12; i++ {
		cat := VerifierCatPass
		if i < 3 {
			cat = VerifierCatInfra
		} else if i == 3 {
			cat = VerifierCatWatchdog
		}
		s.Record(38*time.Second, 6*time.Minute, cat)
	}
	line := FormatVerifierLine(s.Snapshot())
	want := "verifiers: 12 runs, avg wait 38s, avg run 6m, infra 3, watchdog 1\n"
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
	out, err := FormatCapacityText([]byte(`{"pools":[],"verifier":{"runs":2,"wait_avg_seconds":10,"run_avg_seconds":120,"categories":{"infra":1,"watchdog":0}}}`))
	if err != nil || !strings.Contains(out, "verifiers: 2 runs, avg wait 10s, avg run 2m, infra 1, watchdog 0") {
		t.Fatalf("status text: %v %q", err, out)
	}
}
