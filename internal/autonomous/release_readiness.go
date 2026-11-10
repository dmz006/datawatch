package autonomous

import (
	"fmt"
)

// ReleaseComparisonRecord is one comparison of an Automaton's plan against its
// delivered result, used to judge whether a release is ready to ship.
type ReleaseComparisonRecord struct {
	// ID identifies the comparison record.
	ID string `json:"id"`
	// Size is the release size the record belongs to: "patch", "minor" or "major".
	Size string `json:"size"`
	// PlanEqualOrBetter is true when the delivered result matches or exceeds the plan.
	PlanEqualOrBetter bool `json:"plan_equal_or_better"`
	// HasConflicts is true when the comparison surfaced conflicts.
	HasConflicts bool `json:"has_conflicts"`
	// HasPWAChanges is true when the comparison involved PWA changes.
	HasPWAChanges bool `json:"has_pwa_changes"`
	// BlockingDefect is true when the comparison found a defect that blocks release.
	BlockingDefect bool `json:"blocking_defect"`
}

// ReleaseReadinessBar holds the minimum counts a release of one size must reach
// before it is considered ready.
type ReleaseReadinessBar struct {
	// Records is the minimum number of plan-equal-or-better comparison records.
	Records int `json:"records"`
	// Conflicts is the minimum number of counted records that show conflicts.
	Conflicts int `json:"conflicts"`
	// PWAChanges is the minimum number of counted records that show PWA changes.
	PWAChanges int `json:"pwa_changes"`
}

// DefaultReleaseReadinessBar returns the bar used when no other bar is configured:
// 10 records, 2 conflicts and 2 PWA changes.
func DefaultReleaseReadinessBar() ReleaseReadinessBar {
	return ReleaseReadinessBar{Records: 10, Conflicts: 2, PWAChanges: 2}
}

// ReleaseReadiness is the computed readiness of one release size.
type ReleaseReadiness struct {
	// Size is the release size that was evaluated.
	Size string `json:"size"`
	// Records is the number of size-matching records where the plan was equal or better.
	Records int `json:"records"`
	// Conflicts is the number of counted records that show conflicts.
	Conflicts int `json:"conflicts"`
	// PWAChanges is the number of counted records that show PWA changes.
	PWAChanges int `json:"pwa_changes"`
	// BlockingDefects is the number of size-matching records with a blocking defect.
	BlockingDefects int `json:"blocking_defects"`
	// Required is the bar that was applied.
	Required ReleaseReadinessBar `json:"required"`
	// Missing lists one operator-facing sentence per unmet condition.
	Missing []string `json:"missing"`
	// Ready is true only when Missing is empty.
	Ready bool `json:"ready"`
}

// ComputeReleaseReadiness evaluates records of the given release size against bar.
// It returns an error naming the size when size is not "patch", "minor" or "major".
func ComputeReleaseReadiness(size string, records []ReleaseComparisonRecord, bar ReleaseReadinessBar) (ReleaseReadiness, error) {
	switch size {
	case "patch", "minor", "major":
	default:
		return ReleaseReadiness{}, fmt.Errorf("invalid release size %q: must be patch, minor or major", size)
	}

	res := ReleaseReadiness{Size: size, Required: bar}
	for _, r := range records {
		if r.Size != size {
			continue
		}
		if r.BlockingDefect {
			res.BlockingDefects++
		}
		if !r.PlanEqualOrBetter {
			continue
		}
		res.Records++
		if r.HasConflicts {
			res.Conflicts++
		}
		if r.HasPWAChanges {
			res.PWAChanges++
		}
	}

	if res.Records < bar.Records {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"The %s release needs %d Automata comparison records where the plan is equal or better, but has %d.",
			size, bar.Records, res.Records))
	}
	if res.Conflicts < bar.Conflicts {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"The %s release needs %d counted Automaton comparisons with conflicts, but has %d.",
			size, bar.Conflicts, res.Conflicts))
	}
	if res.PWAChanges < bar.PWAChanges {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"The %s release needs %d counted Automaton comparisons with PWA changes, but has %d.",
			size, bar.PWAChanges, res.PWAChanges))
	}
	if res.BlockingDefects > 0 {
		res.Missing = append(res.Missing, fmt.Sprintf(
			"The %s release has %d Automaton comparisons with a blocking defect, which must be resolved.",
			size, res.BlockingDefects))
	}

	res.Ready = len(res.Missing) == 0
	return res, nil
}
