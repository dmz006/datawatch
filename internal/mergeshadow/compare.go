package mergeshadow

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Severity grades how strongly a difference contradicts the plan.
//
// A major difference is one an operator must act on: a version that did
// not land, an expected artifact that is missing, a parity issue the plan
// required that was never filed, or a gate whose landed outcome differs
// from the plan. A minor difference is a wording drift in an artifact or
// a parity issue that landed where the plan did not require one.
type Severity string

// Severities a Difference can carry.
const (
	SeverityMajor Severity = "major"
	SeverityMinor Severity = "minor"
)

// Difference is one field whose planned value and the value the landed
// state actually show do not agree. Planned and Landed are the two sides
// of the disagreement; Landed is "(missing)" when the expected value is
// not on the default branch at all.
type Difference struct {
	Field    string   `json:"field"`
	Planned  string   `json:"planned"`
	Landed   string   `json:"landed"`
	Severity Severity `json:"severity"`
}

// GateOutcome is one gate's landed outcome as written by the supervisor's
// manual record. Name is the gate name the plan uses ("build", "lint" or
// "test"); Status is the outcome the supervisor observed.
type GateOutcome struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Comparison is the result of comparing one Automaton's merge plan with
// the landed values the landed-state task read back. Matched, Different
// and NotCompared count compared fields: version, changelog heading,
// README line, registry row, parity issue and gates. A field counts as
// not compared when its landed side does not exist and no manual gate
// record was supplied (the gate field is the only one that can happen).
// Differences is never nil; an all-matched comparison holds an empty
// one.
type Comparison struct {
	AutomatonID string       `json:"automaton_id"`
	ReleaseSize ReleaseSize  `json:"release_size"`
	Matched     int          `json:"matched"`
	Different   int          `json:"different"`
	NotCompared int          `json:"not_compared"`
	Differences []Difference `json:"differences"`
	CreatedAt   time.Time    `json:"created_at"`
}

// missingMarker is what a Difference shows as the landed value when the
// expected thing is not on the default branch at all.
const missingMarker = "(missing)"

// Compare compares the plan with the landed values the landed-state task
// read back from the default branch: the version, the changelog heading,
// the README line, the registry row, the parity issue presence and the
// gate results.
//
// parityIssuePresent reports whether the landed merge left a datawatch-app
// parity issue. For the gate results the supervisor's manual record
// (manualGates) is the landed side: when manualGates is nil or empty, no
// record exists, the gate field is counted as not compared and no
// difference for it is reported.
//
// A field whose landed value is missing is a difference, not a skip: the
// plan expected it on the default branch and it is not there.
func Compare(plan *MergePlan, landed Landed, parityIssuePresent bool, manualGates []GateOutcome) (Comparison, error) {
	if plan == nil {
		return Comparison{}, errors.New("mergeshadow: nil plan")
	}
	c := Comparison{
		AutomatonID: plan.AutomatonID,
		ReleaseSize: plan.ReleaseSize,
		Differences: []Difference{},
		CreatedAt:   time.Now().UTC(),
	}
	differ := func(d Difference) {
		c.Differences = append(c.Differences, d)
		c.Different++
	}
	match := func() { c.Matched++ }

	// version: every version file the landed-state task read must carry
	// the planned version.
	paths := make([]string, 0, len(landed.Versions))
	for p := range landed.Versions {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	switch {
	case len(paths) == 0:
		differ(Difference{Field: "version", Planned: plan.PlannedVersion, Landed: missingMarker, Severity: SeverityMajor})
	default:
		parts := make([]string, 0, len(paths))
		okAll := true
		for _, p := range paths {
			v := landed.Versions[p]
			if !v.Found {
				okAll = false
			} else if v.Value != plan.PlannedVersion {
				okAll = false
			}
			if v.Found {
				parts = append(parts, p+"="+v.Value)
			} else {
				parts = append(parts, p+"="+missingMarker)
			}
		}
		if okAll {
			match()
		} else {
			differ(Difference{Field: "version", Planned: plan.PlannedVersion, Landed: strings.Join(parts, "; "), Severity: SeverityMajor})
		}
	}

	// changelog heading: wording drift, so a minor difference when the
	// heading exists but differs.
	h := landed.ChangelogHeading
	switch {
	case !h.Found:
		differ(Difference{Field: "changelog_heading", Planned: plan.ChangelogHeading, Landed: missingMarker, Severity: SeverityMajor})
	case h.Value != plan.ChangelogHeading:
		differ(Difference{Field: "changelog_heading", Planned: plan.ChangelogHeading, Landed: h.Value, Severity: SeverityMinor})
	default:
		match()
	}

	// README line.
	r := landed.ReadmeLine
	switch {
	case !r.Found:
		differ(Difference{Field: "readme_line", Planned: plan.ReadmeLine, Landed: missingMarker, Severity: SeverityMajor})
	case r.Value != plan.ReadmeLine:
		differ(Difference{Field: "readme_line", Planned: plan.ReadmeLine, Landed: r.Value, Severity: SeverityMinor})
	default:
		match()
	}

	// registry row.
	g := landed.RegistryRow
	switch {
	case !g.Found:
		differ(Difference{Field: "registry_row", Planned: plan.RegistryRow, Landed: missingMarker, Severity: SeverityMajor})
	case g.Value != plan.RegistryRow:
		differ(Difference{Field: "registry_row", Planned: plan.RegistryRow, Landed: g.Value, Severity: SeverityMinor})
	default:
		match()
	}

	// parity issue presence. A required issue that never landed is major;
	// an issue that landed where the plan did not require one is minor.
	if parityIssuePresent != plan.ParityIssueNeeded {
		sev := SeverityMinor
		if plan.ParityIssueNeeded && !parityIssuePresent {
			sev = SeverityMajor
		}
		differ(Difference{
			Field:    "parity_issue",
			Planned:  strconv.FormatBool(plan.ParityIssueNeeded),
			Landed:   strconv.FormatBool(parityIssuePresent),
			Severity: sev,
		})
	} else {
		match()
	}

	// gates: the supervisor's manual record is the landed side. Without a
	// record the field is not compared.
	if len(manualGates) == 0 {
		c.NotCompared++
	} else {
		byName := make(map[string]string, len(manualGates))
		for _, o := range manualGates {
			byName[o.Name] = o.Status
		}
		gateDiffers := false
		for _, gate := range plan.Gates {
			landedStatus, ok := byName[gate.Name]
			switch {
			case !ok:
				gateDiffers = true
				differ(Difference{Field: "gates", Planned: gate.Name + "=" + gate.Status, Landed: gate.Name + "=" + missingMarker, Severity: SeverityMajor})
			case landedStatus != gate.Status:
				gateDiffers = true
				differ(Difference{Field: "gates", Planned: gate.Name + "=" + gate.Status, Landed: gate.Name + "=" + landedStatus, Severity: SeverityMajor})
			}
		}
		if !gateDiffers {
			match()
		}
	}
	return c, nil
}
