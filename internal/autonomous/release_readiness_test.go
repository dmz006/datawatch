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

// readinessRecords returns n plan-equal-or-better records of size, of which the
// first conflicts and the first pwa carry conflicts and PWA changes.
func readinessRecords(size string, n, conflicts, pwa int) []ReleaseComparisonRecord {
	var out []ReleaseComparisonRecord
	for i := 0; i < n; i++ {
		out = append(out, ReleaseComparisonRecord{
			ID: "r", Size: size, PlanEqualOrBetter: true,
			HasConflicts: i < conflicts, HasPWAChanges: i < pwa,
		})
	}
	return out
}

func TestComputeReleaseReadinessConditions(t *testing.T) {
	bar := DefaultReleaseReadinessBar()

	t.Run("fewer than ten qualifying records lists the missing records and is not ready", func(t *testing.T) {
		res, err := ComputeReleaseReadiness("minor", readinessRecords("minor", 9, 2, 2), bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Ready {
			t.Fatal("expected not ready with 9 of 10 records")
		}
		if len(res.Missing) != 1 {
			t.Fatalf("Missing = %v, want exactly the records sentence", res.Missing)
		}
		if !strings.Contains(res.Missing[0], "10") || !strings.Contains(res.Missing[0], "has 9") {
			t.Errorf("records sentence = %q, want it to name 10 needed and 9 present", res.Missing[0])
		}
	})

	t.Run("fewer than two qualifying records with conflicts lists the missing conflicts", func(t *testing.T) {
		res, err := ComputeReleaseReadiness("minor", readinessRecords("minor", 10, 1, 2), bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Ready {
			t.Fatal("expected not ready with one conflict record")
		}
		if len(res.Missing) != 1 || !strings.Contains(res.Missing[0], "conflicts") {
			t.Fatalf("Missing = %v, want exactly the conflicts sentence", res.Missing)
		}
	})

	t.Run("fewer than two qualifying records with PWA changes lists the missing PWA changes", func(t *testing.T) {
		res, err := ComputeReleaseReadiness("minor", readinessRecords("minor", 10, 2, 1), bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Ready {
			t.Fatal("expected not ready with one PWA record")
		}
		if len(res.Missing) != 1 || !strings.Contains(res.Missing[0], "PWA changes") {
			t.Fatalf("Missing = %v, want exactly the PWA sentence", res.Missing)
		}
	})

	t.Run("one blocking defect fails the bar even when all counts are met", func(t *testing.T) {
		records := readinessRecords("minor", 10, 2, 2)
		records[9].BlockingDefect = true
		res, err := ComputeReleaseReadiness("minor", records, bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Ready {
			t.Fatal("expected not ready with a blocking defect")
		}
		if res.Records != 10 || res.Conflicts != 2 || res.PWAChanges != 2 {
			t.Fatalf("counts = %d/%d/%d, want all met", res.Records, res.Conflicts, res.PWAChanges)
		}
		if len(res.Missing) != 1 || !strings.Contains(res.Missing[0], "blocking defect") {
			t.Fatalf("Missing = %v, want exactly the blocking defect sentence", res.Missing)
		}
	})

	t.Run("a record whose PlanEqualOrBetter is false does not count toward Records", func(t *testing.T) {
		records := readinessRecords("minor", 9, 2, 2)
		records = append(records, ReleaseComparisonRecord{
			ID: "behind", Size: "minor", PlanEqualOrBetter: false,
			HasConflicts: true, HasPWAChanges: true,
		})
		res, err := ComputeReleaseReadiness("minor", records, bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Records != 9 {
			t.Fatalf("Records = %d, want 9", res.Records)
		}
		if res.Ready {
			t.Fatal("expected not ready: the plan-behind record must not count")
		}
		if res.Conflicts != 2 || res.PWAChanges != 2 {
			t.Errorf("Conflicts/PWAChanges = %d/%d, want 2/2 (plan-behind record not counted)", res.Conflicts, res.PWAChanges)
		}
	})

	t.Run("records of another size are ignored", func(t *testing.T) {
		records := readinessRecords("minor", 9, 2, 2)
		records = append(records, readinessRecords("major", 5, 5, 5)...)
		records = append(records, readinessRecords("patch", 5, 5, 5)...)
		res, err := ComputeReleaseReadiness("minor", records, bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Records != 9 || res.Conflicts != 2 || res.PWAChanges != 2 {
			t.Fatalf("counts = %d/%d/%d, want 9/2/2 with other sizes ignored", res.Records, res.Conflicts, res.PWAChanges)
		}
		if res.Ready {
			t.Fatal("expected not ready: other-size records must not fill the minor bar")
		}
	})

	t.Run("an unknown size returns an error", func(t *testing.T) {
		_, err := ComputeReleaseReadiness("huge", readinessRecords("huge", 10, 2, 2), bar)
		if err == nil {
			t.Fatal("expected an error for unknown size \"huge\"")
		}
		if !strings.Contains(err.Error(), `"huge"`) {
			t.Errorf("error %q does not name the size", err)
		}
	})

	t.Run("all conditions met gives Ready true with an empty Missing", func(t *testing.T) {
		res, err := ComputeReleaseReadiness("major", readinessRecords("major", 10, 2, 2), bar)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Ready {
			t.Fatalf("expected ready, got Missing=%v", res.Missing)
		}
		if len(res.Missing) != 0 {
			t.Fatalf("Missing = %v, want empty", res.Missing)
		}
	})
}
