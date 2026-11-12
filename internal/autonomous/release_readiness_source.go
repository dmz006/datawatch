package autonomous

import "strings"

// DifferenceInput is one field-level disagreement in a stored shadow-merge
// comparison: which field, and how strongly the landed value contradicted
// the plan (major is a defect an operator must act on; minor is wording
// drift).
type DifferenceInput struct {
	// Field names the compared field, as the comparison store writes it.
	Field string
	// Severity grades the disagreement: "major" or "minor".
	Severity string
}

// ComparisonInput is one stored shadow-merge comparison in the shape the
// comparison store writes it: the Automaton it was made for, the release
// size its plan proposed, and how the plan's values held up against the
// landed state.
type ComparisonInput struct {
	// AutomatonID identifies the Automaton the comparison was made for.
	AutomatonID string
	// ReleaseSize is the release size the plan proposed: "patch", "minor",
	// "major" or "none".
	ReleaseSize string
	// Different counts the fields whose landed value contradicts the plan.
	Different int
	// Differences holds every disagreement, severity-graded; empty when the
	// plan held up.
	Differences []DifferenceInput
}

// PlanRef is the plan-side fact a readiness record needs for one Automaton,
// joined to its comparison by Automaton id: the project the plan belongs to,
// whether the merge it described would have had conflicts, and whether it
// needed a datawatch-app parity issue (the PWA-change signal).
type PlanRef struct {
	// Project is the project the plan belongs to.
	Project string
	// Conflicts is how many files the merge would touch that need a
	// resolution.
	Conflicts int
	// ParityIssueNeeded is true when the plan required a datawatch-app
	// parity issue, which means the merge involved PWA changes.
	ParityIssueNeeded bool
}

// ReleaseReadinessRecordsFromComparisons turns stored shadow-merge
// comparisons, with their plan facts, into the records
// ComputeReleaseReadiness consumes.
//
// The mapping per comparison:
//
//   - ID is the Automaton id;
//   - Size is the release size the plan proposed, lower-cased;
//   - PlanEqualOrBetter holds when no compared value contradicts the plan
//     (a different count of zero);
//   - HasConflicts holds when the plan's merge had at least one conflict;
//   - HasPWAChanges holds when the plan needed a datawatch-app parity issue;
//   - BlockingDefect holds when the comparison found a major-severity
//     difference.
//
// A comparison whose plan has no stored facts still gets a record, with no
// conflicts and no PWA changes credited to it. When project is non-empty,
// only records whose plan belongs to that project are kept; a comparison
// whose plan is unknown then drops out, because it cannot be attributed.
// The order of the input comparisons is preserved.
func ReleaseReadinessRecordsFromComparisons(
	comparisons []ComparisonInput,
	plans map[string]PlanRef,
	project string,
) []ReleaseComparisonRecord {
	out := make([]ReleaseComparisonRecord, 0, len(comparisons))
	for _, c := range comparisons {
		var plan PlanRef
		if plans != nil {
			plan = plans[c.AutomatonID]
		}
		if project != "" && plan.Project != project {
			continue
		}
		out = append(out, ReleaseComparisonRecord{
			ID:                c.AutomatonID,
			Size:              strings.ToLower(strings.TrimSpace(c.ReleaseSize)),
			PlanEqualOrBetter: c.Different == 0,
			HasConflicts:      plan.Conflicts > 0,
			HasPWAChanges:     plan.ParityIssueNeeded,
			BlockingDefect:    comparisonHasMajorDifference(c.Differences),
		})
	}
	return out
}

func comparisonHasMajorDifference(diffs []DifferenceInput) bool {
	for _, d := range diffs {
		if strings.EqualFold(d.Severity, "major") {
			return true
		}
	}
	return false
}
