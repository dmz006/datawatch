// Package mergeshadow holds the shadow merge plan: a record of what merging an
// Automaton would do (release size, planned version, changelog entry, README
// line, registry row, conflicts, gates and findings) written down without
// applying anything to the repository. A plan is always produced with
// Applied set to false; this feature never changes the repository.
//
// MergePlan is the value type. plan_store.go persists one plan per Automaton
// id as JSON under the daemon data directory and provides List and Prune for
// the read surfaces.
//
// compare.go checks the plan's values (version, changelog heading, README
// line, registry row, parity-issue presence and gate results) against the
// values the landed-state task read back from the default branch, and
// compare_store.go persists one comparison per Automaton in the same style.
//
// defect_candidate.go turns each difference in a comparison into a
// DefectCandidate with a stable key and persists it in the same style. It
// only records candidates; filing them is left to the defect intake.
package mergeshadow

import "time"

// ReleaseSize is the semantic-version bump a merge plan proposes.
type ReleaseSize string

// Release sizes a plan can propose.
const (
	ReleaseSizePatch ReleaseSize = "patch"
	ReleaseSizeMinor ReleaseSize = "minor"
	ReleaseSizeMajor ReleaseSize = "major"
	ReleaseSizeNone  ReleaseSize = "none"
)

// Valid reports whether s is one of the four release sizes.
func (s ReleaseSize) Valid() bool {
	switch s {
	case ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor, ReleaseSizeNone:
		return true
	}
	return false
}

// ChangelogSourceMissing is the changelog_source value used when no entry task
// id could be found for the Automaton.
const ChangelogSourceMissing = "missing"

// Conflict is one file the merge would touch that needs a resolution.
type Conflict struct {
	Path       string `json:"path"`
	FileClass  string `json:"file_class"`
	Resolution string `json:"resolution"`
	ResolvedBy string `json:"resolved_by"`
}

// Gate is the outcome of one check run as part of planning.
type Gate struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"`
	Duration time.Duration `json:"duration"`
	Detail   string        `json:"detail"`
}

// Finding is a problem the planner found. A blocking finding stops the merge.
type Finding struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking"`
}

// MergePlan describes what merging one Automaton would do. Applied is always
// false in this feature; the store refuses to save a plan with Applied set.
type MergePlan struct {
	AutomatonID      string      `json:"automaton_id"`
	Project          string      `json:"project"`
	Branch           string      `json:"branch"`
	BaseRef          string      `json:"base_ref"`
	ReleaseSize      ReleaseSize `json:"release_size"`
	PlannedVersion   string      `json:"planned_version"`
	ChangelogHeading string      `json:"changelog_heading"`
	ChangelogBody    string      `json:"changelog_body"`
	// ChangelogSource is the entry task id, or ChangelogSourceMissing.
	ChangelogSource   string     `json:"changelog_source"`
	ReadmeLine        string     `json:"readme_line"`
	RegistryRow       string     `json:"registry_row"`
	ParityIssueNeeded bool       `json:"parity_issue_needed"`
	Conflicts         []Conflict `json:"conflicts"`
	Gates             []Gate     `json:"gates"`
	Findings          []Finding  `json:"findings"`
	CreatedAt         time.Time  `json:"created_at"`
	Applied           bool       `json:"applied"`
	// IntermittentDefects are the defect records this run created for
	// intermittent gates. They are stored in their own store, not in the plan
	// file, so the plan JSON is unchanged.
	IntermittentDefects []IntermittentDefect `json:"-"`
}
