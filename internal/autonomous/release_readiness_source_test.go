package autonomous

import "testing"

func TestReleaseReadinessRecordsFromComparisonsMapsFields(t *testing.T) {
	comparisons := []ComparisonInput{
		{
			AutomatonID: "a1",
			ReleaseSize: "Patch",
			Different:   0,
			Differences: nil,
		},
		{
			AutomatonID: "a2",
			ReleaseSize: "minor",
			Different:   2,
			Differences: []DifferenceInput{
				{Field: "version", Severity: "major"},
				{Field: "readme", Severity: "minor"},
			},
		},
		{
			AutomatonID: "a3",
			ReleaseSize: "major",
			Different:   1,
			Differences: []DifferenceInput{{Field: "parity", Severity: "Minor"}},
		},
	}
	plans := map[string]PlanRef{
		"a1": {Project: "demo", Conflicts: 2, ParityIssueNeeded: true},
		"a2": {Project: "demo"},
		// a3 has no stored plan facts.
	}

	recs := ReleaseReadinessRecordsFromComparisons(comparisons, plans, "")
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d", len(recs))
	}
	want := []ReleaseComparisonRecord{
		{ID: "a1", Size: "patch", PlanEqualOrBetter: true, HasConflicts: true, HasPWAChanges: true, BlockingDefect: false},
		{ID: "a2", Size: "minor", PlanEqualOrBetter: false, HasConflicts: false, HasPWAChanges: false, BlockingDefect: true},
		{ID: "a3", Size: "major", PlanEqualOrBetter: false, HasConflicts: false, HasPWAChanges: false, BlockingDefect: false},
	}
	for i := range want {
		if recs[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, recs[i], want[i])
		}
	}

	// A project filter keeps only the records whose plan belongs to it, and
	// drops comparisons whose plan is unknown.
	if recs = ReleaseReadinessRecordsFromComparisons(comparisons, plans, "demo"); len(recs) != 2 {
		t.Errorf("project demo: want 2 records, got %d", len(recs))
	}
	if recs = ReleaseReadinessRecordsFromComparisons(comparisons, plans, "none"); len(recs) != 0 {
		t.Errorf("project none: want 0 records, got %d", len(recs))
	}

	// A nil plans map must be usable: no facts, no crash.
	if recs = ReleaseReadinessRecordsFromComparisons(comparisons, nil, ""); len(recs) != 3 {
		t.Errorf("nil plans: want 3 records, got %d", len(recs))
	}
	if recs[0].HasConflicts || recs[0].HasPWAChanges || recs[0].BlockingDefect {
		t.Errorf("nil plans: nothing should be credited: %+v", recs[0])
	}
}
