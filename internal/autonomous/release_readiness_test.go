package autonomous

import (
	"strings"
	"testing"
)

func TestDefaultReleaseReadinessBar(t *testing.T) {
	got := DefaultReleaseReadinessBar()
	want := ReleaseReadinessBar{Records: 10, Conflicts: 2, PWAChanges: 2}
	if got != want {
		t.Fatalf("DefaultReleaseReadinessBar() = %+v, want %+v", got, want)
	}
}

func TestComputeReleaseReadiness_InvalidSize(t *testing.T) {
	for _, size := range []string{"", "huge", "Patch"} {
		_, err := ComputeReleaseReadiness(size, nil, DefaultReleaseReadinessBar())
		if err == nil {
			t.Fatalf("size %q: expected error", size)
		}
		if !strings.Contains(err.Error(), `"`+size+`"`) {
			t.Errorf("size %q: error %q does not name the size", size, err)
		}
	}
}

func TestComputeReleaseReadiness_ReadyWhenBarMet(t *testing.T) {
	var records []ReleaseComparisonRecord
	for i := 0; i < 10; i++ {
		records = append(records, ReleaseComparisonRecord{
			ID: "r", Size: "minor", PlanEqualOrBetter: true,
			HasConflicts: i < 2, HasPWAChanges: i < 2,
		})
	}
	res, err := ComputeReleaseReadiness("minor", records, DefaultReleaseReadinessBar())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Ready || len(res.Missing) != 0 {
		t.Fatalf("expected ready, got Ready=%v Missing=%v", res.Ready, res.Missing)
	}
	if res.Records != 10 || res.Conflicts != 2 || res.PWAChanges != 2 || res.BlockingDefects != 0 {
		t.Errorf("counts = %+v", res)
	}
	if res.Size != "minor" || res.Required != DefaultReleaseReadinessBar() {
		t.Errorf("Size/Required = %q/%+v", res.Size, res.Required)
	}
}

func TestComputeReleaseReadiness_FiltersBySizeAndCounts(t *testing.T) {
	records := []ReleaseComparisonRecord{
		{ID: "a", Size: "patch", PlanEqualOrBetter: true, HasConflicts: true, HasPWAChanges: true},
		{ID: "b", Size: "patch", PlanEqualOrBetter: false, HasConflicts: true, HasPWAChanges: true},
		{ID: "c", Size: "patch", PlanEqualOrBetter: false, BlockingDefect: true},
		{ID: "d", Size: "major", PlanEqualOrBetter: true, BlockingDefect: true},
		{ID: "e", Size: "patch", PlanEqualOrBetter: true, BlockingDefect: true},
	}
	res, err := ComputeReleaseReadiness("patch", records, ReleaseReadinessBar{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Records != 2 {
		t.Errorf("Records = %d, want 2", res.Records)
	}
	if res.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", res.Conflicts)
	}
	if res.PWAChanges != 1 {
		t.Errorf("PWAChanges = %d, want 1", res.PWAChanges)
	}
	if res.BlockingDefects != 2 {
		t.Errorf("BlockingDefects = %d, want 2 (patch-only)", res.BlockingDefects)
	}
	if res.Ready {
		t.Errorf("expected not ready because of blocking defects")
	}
}

func TestComputeReleaseReadiness_MissingSentences(t *testing.T) {
	res, err := ComputeReleaseReadiness("major", nil, DefaultReleaseReadinessBar())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready {
		t.Fatal("expected not ready")
	}
	if len(res.Missing) != 3 {
		t.Fatalf("Missing = %v, want 3 entries", res.Missing)
	}
	for _, m := range res.Missing {
		if strings.Contains(m, "PRD") {
			t.Errorf("missing sentence names PRD: %q", m)
		}
	}
	if !strings.Contains(res.Missing[0], "Automata") {
		t.Errorf("records sentence should name Automata: %q", res.Missing[0])
	}
	if !strings.Contains(res.Missing[1], "Automaton") {
		t.Errorf("conflicts sentence should name Automaton: %q", res.Missing[1])
	}
	if !strings.Contains(res.Missing[2], "Automaton") {
		t.Errorf("PWA sentence should name Automaton: %q", res.Missing[2])
	}
}

func TestComputeReleaseReadiness_BlockingDefectMissing(t *testing.T) {
	bar := ReleaseReadinessBar{Records: 1, Conflicts: 0, PWAChanges: 0}
	records := []ReleaseComparisonRecord{
		{ID: "a", Size: "patch", PlanEqualOrBetter: true, BlockingDefect: true},
	}
	res, err := ComputeReleaseReadiness("patch", records, bar)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready {
		t.Fatal("expected not ready with a blocking defect")
	}
	if len(res.Missing) != 1 || !strings.Contains(res.Missing[0], "blocking defect") {
		t.Fatalf("Missing = %v", res.Missing)
	}
}

func TestComputeReleaseReadiness_ZeroBarIsReady(t *testing.T) {
	res, err := ComputeReleaseReadiness("patch", nil, ReleaseReadinessBar{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Ready || res.Missing != nil {
		t.Fatalf("expected ready with empty bar, got %+v", res)
	}
}
