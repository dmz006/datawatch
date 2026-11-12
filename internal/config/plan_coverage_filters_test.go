package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPlanCoverageFilters_ConfigUnset: a zero config resolves to both
// filters on, the default sections, and the default minimum length.
func TestPlanCoverageFilters_ConfigUnset(t *testing.T) {
	var a AutonomousConfig
	got := a.EffectivePlanCoverageFilters()
	want := PlanCoverageFilters{
		SuppressCreatedFiles: true,
		ScopedRequirements:   true,
		RequirementSections:  []string{"scope", "constraints"},
		MinRequirementChars:  30,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unset config: got %+v, want %+v", got, want)
	}
}

// TestPlanCoverageFilters_ConfigExplicitFalse: an explicit off is
// honoured for both filter switches.
func TestPlanCoverageFilters_ConfigExplicitFalse(t *testing.T) {
	a := AutonomousConfig{
		PlanCoverageSuppressCreatedFiles: boolPtr(false),
		PlanCoverageScopedRequirements:   boolPtr(false),
	}
	got := a.EffectivePlanCoverageFilters()
	if got.SuppressCreatedFiles {
		t.Error("explicit false: SuppressCreatedFiles should be off")
	}
	if got.ScopedRequirements {
		t.Error("explicit false: ScopedRequirements should be off")
	}
}

// TestPlanCoverageFilters_ConfigSectionsNormalization: mixed case,
// extra whitespace and duplicate section words resolve to clean,
// deduplicated, lowercased entries (first position wins).
func TestPlanCoverageFilters_ConfigSectionsNormalization(t *testing.T) {
	a := AutonomousConfig{PlanCoverageRequirementSections: "  Scope , ,SCOPE,Requirements "}
	got := a.EffectivePlanCoverageFilters()
	want := []string{"scope", "requirements"}
	if !reflect.DeepEqual(got.RequirementSections, want) {
		t.Fatalf("sections: got %v, want %v", got.RequirementSections, want)
	}
}

// TestPlanCoverageFilters_ConfigBlankSections: an empty (or all-whitespace)
// section list falls back to the package defaults.
func TestPlanCoverageFilters_ConfigBlankSections(t *testing.T) {
	for _, raw := range []string{"", "  , ,  "} {
		a := AutonomousConfig{PlanCoverageRequirementSections: raw}
		got := a.EffectivePlanCoverageFilters()
		want := []string{"scope", "constraints"}
		if !reflect.DeepEqual(got.RequirementSections, want) {
			t.Fatalf("sections %q: got %v, want %v", raw, got.RequirementSections, want)
		}
	}
}

// TestPlanCoverageFilters_ConfigMinChars: zero and negative minimums
// resolve to the default 30; a positive value is kept as-is.
func TestPlanCoverageFilters_ConfigMinChars(t *testing.T) {
	for _, tc := range []struct {
		raw  int
		want int
	}{
		{0, 30},
		{-5, 30},
		{80, 80},
	} {
		a := AutonomousConfig{PlanCoverageMinRequirementChars: tc.raw}
		got := a.EffectivePlanCoverageFilters()
		if got.MinRequirementChars != tc.want {
			t.Errorf("minChars %d: got %d, want %d", tc.raw, got.MinRequirementChars, tc.want)
		}
	}
}

// TestPlanCoverageFilters_ConfigDefaultSliceIsCopy: mutating the
// RequirementSections slice returned for a blank config must not change
// the package default.
func TestPlanCoverageFilters_ConfigDefaultSliceIsCopy(t *testing.T) {
	before := append([]string(nil), DefaultPlanCoverageRequirementSections...)
	got := EffectivePlanCoverageFilters(nil, nil, "", 0)
	got.RequirementSections[0] = "mutated"
	got.RequirementSections = append(got.RequirementSections, "also-mutated")
	if !reflect.DeepEqual(DefaultPlanCoverageRequirementSections, before) {
		t.Fatalf("package default mutated: %v", DefaultPlanCoverageRequirementSections)
	}
}

// TestPlanCoverageFilters_ConfigRoundTripYAML: an explicit off on both
// filter pointers survives a marshal/unmarshal round trip through the
// same YAML package the config package uses everywhere else.
func TestPlanCoverageFilters_ConfigRoundTripYAML(t *testing.T) {
	in := AutonomousConfig{
		PlanCoverageSuppressCreatedFiles: boolPtr(false),
		PlanCoverageScopedRequirements:   boolPtr(false),
	}
	b, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var out AutonomousConfig
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if out.PlanCoverageSuppressCreatedFiles == nil || *out.PlanCoverageSuppressCreatedFiles {
		t.Errorf("yaml round trip: SuppressCreatedFiles lost its explicit off (got %v)", out.PlanCoverageSuppressCreatedFiles)
	}
	if out.PlanCoverageScopedRequirements == nil || *out.PlanCoverageScopedRequirements {
		t.Errorf("yaml round trip: ScopedRequirements lost its explicit off (got %v)", out.PlanCoverageScopedRequirements)
	}
}

// TestPlanCoverageFilters_ConfigRoundTripJSON: the same explicit-off
// guarantee through encoding/json, mirroring the server config round
// trip.
func TestPlanCoverageFilters_ConfigRoundTripJSON(t *testing.T) {
	in := AutonomousConfig{
		PlanCoverageSuppressCreatedFiles: boolPtr(false),
		PlanCoverageScopedRequirements:   boolPtr(false),
	}
	b, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var out AutonomousConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if out.PlanCoverageSuppressCreatedFiles == nil || *out.PlanCoverageSuppressCreatedFiles {
		t.Errorf("json round trip: SuppressCreatedFiles lost its explicit off (got %v)", out.PlanCoverageSuppressCreatedFiles)
	}
	if out.PlanCoverageScopedRequirements == nil || *out.PlanCoverageScopedRequirements {
		t.Errorf("json round trip: ScopedRequirements lost its explicit off (got %v)", out.PlanCoverageScopedRequirements)
	}
}
