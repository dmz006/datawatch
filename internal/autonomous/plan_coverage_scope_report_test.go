package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// requirementsSpec shapes an operator feature request: a READ FIRST list
// carrying a file name and a backend key token, a hyphenated phrase
// quoted from another feature, and the genuine SCOPE and Constraints
// sections. The plan below never mentions the stray tokens or either of
// the two SCOPE sentences, and only partly covers the Constraints
// sentence.
const requirementsSpec = `FEATURE (operator, 2026-11-02): the importer batches node writes per day.
READ FIRST: the retry_backoff policy in internal/autonomous/retry_backoff.go, plus the "shard-rollup" semantics quoted from the earlier exporter feature.
SCOPE, EXACTLY:
The importer must retry with jitter on transient errors.
Backoff must stay bounded.
Constraints:
Every ledger flush must close the shard before the node drains.`

var requirementsPlan = []Story{{
	Title: "Ledger flush hardening",
	Tasks: []Task{{
		Title: "Flush the exporter ledger",
		Spec:  "The exporter keeps the ledger and performs a flush before exit",
	}},
}}

// unscopedRequirementsReport is what the unscoped gate reports for the
// fixture: every requirement unmatched, the Constraints one only partly.
var unscopedFixtureReport = &PlanCoverageReport{
	Missing:    5,
	Weakened:   1,
	Suppressed: 0,
	Unmatched: []RequirementVerdict{
		{Requirement: "The importer must retry with jitter on transient errors.", Verdict: CoverageMissing},
		{Requirement: "Backoff must stay bounded.", Verdict: CoverageMissing},
		{Requirement: "Every ledger flush must close the shard before the node drains.", Verdict: CoverageWeakened},
		{Requirement: "internal/autonomous/retry_backoff.go", Verdict: CoverageMissing},
		{Requirement: "shard-rollup", Verdict: CoverageMissing},
		{Requirement: "retry_backoff", Verdict: CoverageMissing},
	},
}

func TestPlanCoverageFilters_RequirementsScopedSuppressesStrayTokens(t *testing.T) {
	sections := []string{"scope", "constraints"}
	scoped := CheckPlanCoverageWithOptions(requirementsSpec, requirementsPlan, PlanCoverageOptions{
		Scoped:   true,
		Sections: sections,
	})

	// Covered, Missing, Weakened and Unmatched describe only the scoped
	// requirements: the two genuine SCOPE sentences (the plan never
	// mentions them -> missing) and the genuinely-only-partly-covered
	// Constraints sentence (ledger and flush present, the rest not ->
	// weakened).
	if scoped.Covered != 0 || scoped.Missing != 2 || scoped.Weakened != 1 {
		t.Fatalf("scoped counts = covered %d, missing %d, weakened %d; want 0/2/1; report: %+v",
			scoped.Covered, scoped.Missing, scoped.Weakened, scoped)
	}
	if len(scoped.Unmatched) != 3 {
		t.Fatalf("scoped Unmatched = %v; want 3 entries", scoped.Unmatched)
	}
	if !strings.Contains(scoped.Unmatched[0].Requirement, "retry with jitter on transient errors") || scoped.Unmatched[0].Verdict != CoverageMissing {
		t.Errorf("first scoped unmatched = %+v; want the genuine SCOPE must-sentence, missing", scoped.Unmatched[0])
	}
	if !strings.Contains(scoped.Unmatched[1].Requirement, "Backoff must stay bounded") || scoped.Unmatched[1].Verdict != CoverageMissing {
		t.Errorf("second scoped unmatched = %+v; want the second SCOPE sentence, missing", scoped.Unmatched[1])
	}
	if !strings.Contains(scoped.Unmatched[2].Requirement, "close the shard") || scoped.Unmatched[2].Verdict != CoverageWeakened {
		t.Errorf("third scoped unmatched = %+v; want the Constraints sentence, weakened", scoped.Unmatched[2])
	}

	// The READ FIRST stray tokens are nowhere in the scoped report...
	for _, token := range []string{"internal/autonomous/retry_backoff.go", "retry_backoff", "shard-rollup"} {
		for _, v := range scoped.Unmatched {
			if strings.Contains(strings.ToLower(v.Requirement), strings.ToLower(token)) {
				t.Errorf("scoped Unmatched carries the stray token %q: %q", token, v.Requirement)
			}
		}
	}

	// ...but the unscoped gate still flags them, and scoping reports them
	// as suppressed: exactly the three stray tokens, since the other
	// unscoped findings are the scoped requirements themselves.
	unscoped := CheckPlanCoverageWithOptions(requirementsSpec, requirementsPlan, PlanCoverageOptions{})
	if !reflect.DeepEqual(unscoped, unscopedFixtureReport) {
		t.Fatalf("unscoped report:\n got  %+v\n want %v", unscoped, unscopedFixtureReport)
	}
	for _, token := range []string{"internal/autonomous/retry_backoff.go", "retry_backoff", "shard-rollup"} {
		found := false
		for _, v := range unscoped.Unmatched {
			if v.Requirement == token {
				found = true
			}
		}
		if !found {
			t.Errorf("unscoped Unmatched lacks the stray token %q", token)
		}
	}
	if have := scoped.Suppressed; have != 3 {
		t.Errorf("scoped Suppressed = %d; want 3 (the stray tokens the unscoped gate would have flagged)", have)
	}
	if unscoped.Suppressed != 0 {
		t.Errorf("unscoped Suppressed = %d; want 0", unscoped.Suppressed)
	}
}

func TestPlanCoverageFilters_RequirementsScopedFalseReproducesUnscoped(t *testing.T) {
	legacy := CheckPlanCoverage(requirementsSpec, requirementsPlan)
	if !reflect.DeepEqual(legacy, unscopedFixtureReport) {
		t.Fatalf("CheckPlanCoverage:\n got  %+v\n want %v", legacy, unscopedFixtureReport)
	}
	zero := CheckPlanCoverageWithOptions(requirementsSpec, requirementsPlan, PlanCoverageOptions{})
	if !reflect.DeepEqual(zero, unscopedFixtureReport) {
		t.Fatalf("zero-options CheckPlanCoverageWithOptions:\n got  %+v\n want %v", zero, unscopedFixtureReport)
	}
}

func TestPlanCoverageFilters_RequirementsNoRecognisedHeadingUsesWholeSpec(t *testing.T) {
	spec := "The writer must retry transient fetches before it ships. It reads docs/notes.txt whenever asked."
	plan := []Story{{Title: "Ship the build"}}

	scoped := CheckPlanCoverageWithOptions(spec, plan, PlanCoverageOptions{
		Scoped:   true,
		Sections: []string{"scope", "constraints"},
	})
	// No recognised heading: the whole spec is the source, but the
	// loose token never counts on its own.
	if scoped.Covered != 0 || scoped.Missing != 1 || scoped.Weakened != 0 {
		t.Fatalf("scoped counts unusual: %+v", scoped)
	}
	if len(scoped.Unmatched) != 1 || !strings.Contains(scoped.Unmatched[0].Requirement, "retry transient fetches") {
		t.Fatalf("scoped Unmatched = %v; want only the obligation sentence", scoped.Unmatched)
	}

	unscoped := CheckPlanCoverage(spec, plan)
	if len(unscoped.Unmatched) != 2 {
		t.Fatalf("unscoped Unmatched = %v; want the sentence and the loose token", unscoped.Unmatched)
	}
	found := false
	for _, v := range unscoped.Unmatched {
		if v.Requirement == "docs/notes.txt" && v.Verdict == CoverageMissing {
			found = true
		}
	}
	if !found {
		t.Errorf("unscoped Unmatched lacks the loose token docs/notes.txt")
	}
	if scoped.Suppressed != 1 {
		t.Errorf("scoped Suppressed = %d; want 1 (the loose token)", scoped.Suppressed)
	}
}

func TestPlanCoverageFilters_RequirementsMinCharsDropsShortAndCounts(t *testing.T) {
	spec := "SCOPE, EXACTLY:\n  the pipeline must stay small.\nThe importer must flush the ledger before every drain."
	sections := []string{"scope", "constraints"}
	plan := []Story{{Title: "Rotate the nightly exporter"}}

	zero := CheckPlanCoverageWithOptions(spec, plan, PlanCoverageOptions{Scoped: true, Sections: sections, MinChars: 0})
	capped := CheckPlanCoverageWithOptions(spec, plan, PlanCoverageOptions{Scoped: true, Sections: sections, MinChars: 30})

	// The short obligation sentence is under the 30-char floor: kept at
	// MinChars 0, dropped at 30. The longer one is kept in both.
	if len(zero.Unmatched) != 2 {
		t.Fatalf("MinChars=0 Unmatched = %v; want both sentences", zero.Unmatched)
	}
	if len(capped.Unmatched) != 1 || !strings.Contains(capped.Unmatched[0].Requirement, "flush the ledger") {
		t.Fatalf("MinChars=30 Unmatched = %v; want only the longer sentence", capped.Unmatched)
	}
	for _, v := range zero.Unmatched {
		if strings.Contains(v.Requirement, "stay small") && v.Verdict != CoverageMissing {
			t.Errorf("the short sentence should be missing against this plan; got %q", v)
		}
	}

	// The short sentence is an unscoped finding that scoping removed, so
	// it raises Suppressed by exactly one when the floor drops it.
	if want := zero.Suppressed + 1; capped.Suppressed != want {
		t.Errorf("Suppressed(MinChars=30) = %d; want %d", capped.Suppressed, want)
	}

	// At MinChars 0 nothing is suppressed: both sentences are in the
	// scoped list, and neither is a loose token of the unscoped gate.
	if zero.Suppressed != 0 {
		t.Errorf("Suppressed(MinChars=0) = %d; want 0", zero.Suppressed)
	}
}

func TestPlanCoverageFilters_RequirementsDeterministic(t *testing.T) {
	sections := []string{"scope", "constraints"}
	opts := PlanCoverageOptions{Scoped: true, Sections: sections}

	a := CheckPlanCoverageWithOptions(requirementsSpec, requirementsPlan, opts)
	b := CheckPlanCoverageWithOptions(requirementsSpec, requirementsPlan, opts)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("scoped reports differ:\n a: %+v\n b: %+v", a, b)
	}
	c := CheckPlanCoverage(requirementsSpec, requirementsPlan)
	d := CheckPlanCoverage(requirementsSpec, requirementsPlan)
	if !reflect.DeepEqual(c, d) {
		t.Fatalf("unscoped reports differ:\n c: %+v\n d: %+v", c, d)
	}
}
