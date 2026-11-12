package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestVerifierRunCapStatsCountCapEndOnce(t *testing.T) {
	s := NewVerifierStats(nil)
	cat := VerifierCategoryForRunCap(false, "infra", "verifier(session): run cap", false, true)
	if cat != VerifierCatRunCap {
		t.Fatalf("category = %q, want %q", cat, VerifierCatRunCap)
	}
	s.Record(time.Second, time.Minute, cat)
	v := s.Snapshot()
	if v.RunCapEnds != 1 {
		t.Fatalf("RunCapEnds = %d, want 1", v.RunCapEnds)
	}
	if v.Categories[VerifierCatRunCap] != 1 {
		t.Fatalf("Categories[run_cap] = %d, want 1", v.Categories[VerifierCatRunCap])
	}
	if v.WatchdogEnds != 0 || v.Escalations != 0 {
		t.Fatalf("watchdog=%d escalations=%d, want 0 0", v.WatchdogEnds, v.Escalations)
	}
}

func TestVerifierRunCapStatsCountWatchdogAndPassUnchanged(t *testing.T) {
	s := NewVerifierStats(nil)
	s.Record(0, time.Minute, VerifierCategoryForRunCap(false, "infra", "verifier no-progress watchdog", false, false))
	s.Record(0, time.Minute, VerifierCategoryForRunCap(true, "", "", false, false))
	v := s.Snapshot()
	if v.RunCapEnds != 0 || v.Categories[VerifierCatRunCap] != 0 {
		t.Fatalf("RunCapEnds=%d run_cap=%d, want 0 0", v.RunCapEnds, v.Categories[VerifierCatRunCap])
	}
	if v.WatchdogEnds != 1 || v.Categories[VerifierCatWatchdog] != 1 {
		t.Fatalf("WatchdogEnds=%d, want 1", v.WatchdogEnds)
	}
}

func TestVerifierRunCapStatsCountCategoryForWithoutCap(t *testing.T) {
	if got := VerifierCategoryFor(false, "infra", "verifier no-progress watchdog", false); got != VerifierCatWatchdog {
		t.Fatalf("watchdog = %q", got)
	}
	if got := VerifierCategoryFor(true, "", "", false); got != VerifierCatPass {
		t.Fatalf("pass = %q", got)
	}
}

func TestVerifierRunCapStatsCountCategoriesListed(t *testing.T) {
	found := false
	for _, c := range VerifierCategories {
		if c == VerifierCatRunCap {
			found = true
		}
	}
	if !found || VerifierCatRunCap != "run_cap" {
		t.Fatalf("VerifierCategories missing run_cap: %v", VerifierCategories)
	}
	if got := NewVerifierStats(nil).Snapshot().Categories["run_cap"]; got != 0 {
		t.Fatalf("empty snapshot run_cap = %d", got)
	}
}

func TestVerifierRunCapStatsCountLineShowsOnlyWhenNonZero(t *testing.T) {
	s := NewVerifierStats(nil)
	s.Record(0, time.Minute, VerifierCatPass)
	if line := FormatVerifierLine(s.Snapshot()); strings.Contains(line, "run_cap") {
		t.Fatalf("line without cap ends mentions run_cap: %q", line)
	}
	s.Record(0, time.Minute, VerifierCatRunCap)
	if line := FormatVerifierLine(s.Snapshot()); !strings.Contains(line, "run_cap 1") {
		t.Fatalf("line with cap end = %q, want run_cap 1", line)
	}
}
