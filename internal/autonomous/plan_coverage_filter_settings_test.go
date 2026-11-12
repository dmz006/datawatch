// plan_coverage_filter_settings_test.go — the autonomous Manager's
// effective plan-coverage finding-filter resolution: an unset config
// resolves to both filters on, the default sections [scope constraints]
// and a minimum requirement length of 30; explicit config values
// resolve as given; and a manager Config JSON round-trip keeps an
// explicit false and a sections string (no omitempty may elide them).
package autonomous

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestPlanCoverageFilters_ManagerDefaults proves a zero-value Manager
// resolves to both filters on, the default sections [scope constraints]
// and the default minimum length of 30.
func TestPlanCoverageFilters_ManagerDefaults(t *testing.T) {
	m := &Manager{}
	f := m.EffectivePlanCoverageFilters()
	if !f.SuppressCreatedFiles {
		t.Errorf("SuppressCreatedFiles = false on unset config, want true (filter on)")
	}
	if !f.ScopedRequirements {
		t.Errorf("ScopedRequirements = false on unset config, want true (filter on)")
	}
	if want := []string{"scope", "constraints"}; !reflect.DeepEqual(f.RequirementSections, want) {
		t.Errorf("RequirementSections = %v, want %v", f.RequirementSections, want)
	}
	if f.MinRequirementChars != 30 {
		t.Errorf("MinRequirementChars = %d, want 30", f.MinRequirementChars)
	}
}

// TestPlanCoverageFilters_ManagerExplicitValues proves explicit config
// values resolve as given: both filters off, the operator's own
// sections (case-normalized, order kept) and a non-default minimum.
func TestPlanCoverageFilters_ManagerExplicitValues(t *testing.T) {
	m := &Manager{cfg: Config{
		PlanCoverageSuppressCreatedFiles: boolPtr(false),
		PlanCoverageScopedRequirements:   boolPtr(false),
		PlanCoverageRequirementSections:  "Scope,Tests",
		PlanCoverageMinRequirementChars:  80,
	}}
	f := m.EffectivePlanCoverageFilters()
	if f.SuppressCreatedFiles {
		t.Errorf("SuppressCreatedFiles = true, want false (explicit off)")
	}
	if f.ScopedRequirements {
		t.Errorf("ScopedRequirements = true, want false (explicit off)")
	}
	if want := []string{"scope", "tests"}; !reflect.DeepEqual(f.RequirementSections, want) {
		t.Errorf("RequirementSections = %v, want %v", f.RequirementSections, want)
	}
	if f.MinRequirementChars != 80 {
		t.Errorf("MinRequirementChars = %d, want 80", f.MinRequirementChars)
	}
}

// TestPlanCoverageFilters_ManagerConfigJSONRoundTrip proves a manager
// Config survives a JSON marshal/unmarshal with an explicit false intact
// and the sections string preserved — the handleAutonomousConfig live
// round-trip depends on this, so no omitempty may elide the keys.
func TestPlanCoverageFilters_ManagerConfigJSONRoundTrip(t *testing.T) {
	in := Config{
		PlanCoverageSuppressCreatedFiles: boolPtr(false), // explicit off must survive
		PlanCoverageScopedRequirements:   boolPtr(false),
		PlanCoverageRequirementSections:  "Scope,Tests",
		PlanCoverageMinRequirementChars:  80,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal Config: %v", err)
	}
	var out Config
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal Config: %v", err)
	}
	if out.PlanCoverageSuppressCreatedFiles == nil || *out.PlanCoverageSuppressCreatedFiles {
		t.Errorf("SuppressCreatedFiles after round-trip = %v, want explicit false", out.PlanCoverageSuppressCreatedFiles)
	}
	if out.PlanCoverageScopedRequirements == nil || *out.PlanCoverageScopedRequirements {
		t.Errorf("ScopedRequirements after round-trip = %v, want explicit false", out.PlanCoverageScopedRequirements)
	}
	if out.PlanCoverageRequirementSections != "Scope,Tests" {
		t.Errorf("RequirementSections = %q after round-trip, want %q", out.PlanCoverageRequirementSections, "Scope,Tests")
	}
	if out.PlanCoverageMinRequirementChars != 80 {
		t.Errorf("MinRequirementChars = %d after round-trip, want 80", out.PlanCoverageMinRequirementChars)
	}

	// The resolved filters from the round-tripped config must match the
	// originals: both filters off, sections [scope tests] and minimum 80.
	if f := (&Manager{cfg: out}).EffectivePlanCoverageFilters(); f.SuppressCreatedFiles || f.ScopedRequirements ||
		!reflect.DeepEqual(f.RequirementSections, []string{"scope", "tests"}) || f.MinRequirementChars != 80 {
		t.Errorf("effective filters after round-trip = %+v", f)
	}
}
