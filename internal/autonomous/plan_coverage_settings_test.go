// plan_coverage_settings_test.go — the autonomous Manager's effective
// plan-coverage settings resolution: unset config behaves as mode
// "block" + min ratio 1.0, off/warn are accepted, unknown modes are
// rejected, and a zero ratio normalizes to 1.0.
package autonomous

import (
	"encoding/json"
	"testing"
)

func TestManagerEffectivePlanCoverageDefault(t *testing.T) {
	// Zero-value manager: every config key unset. Must behave as
	// check mode "block" and min ratio 1.0.
	m := &Manager{}
	s, err := m.EffectivePlanCoverage()
	if err != nil {
		t.Fatalf("EffectivePlanCoverage on unset config: %v", err)
	}
	if s.Mode != "block" {
		t.Fatalf("default mode = %q, want \"block\"", s.Mode)
	}
	if s.MinRatio != 1.0 {
		t.Fatalf("default min ratio = %v, want 1.0", s.MinRatio)
	}
}

func TestManagerEffectivePlanCoverageExplicitModes(t *testing.T) {
	cases := []struct {
		mode  string
		ratio float64
		wantM string
		wantR float64
	}{
		{"off", 0, "off", 1.0}, // zero ratio normalizes to 1.0
		{"warn", 0.5, "warn", 0.5},
		{"block", 0.9, "block", 0.9},
		{"", 0, "block", 1.0},      // unset -> defaults
		{"WARN", 1.5, "warn", 1.0}, // case-insensitive, >1 clamps
	}
	for _, c := range cases {
		m := &Manager{cfg: Config{PlanCoverageCheck: c.mode, PlanCoverageMinRatio: c.ratio}}
		s, err := m.EffectivePlanCoverage()
		if err != nil {
			t.Fatalf("EffectivePlanCoverage(%q/%v): %v", c.mode, c.ratio, err)
		}
		if s.Mode != c.wantM || s.MinRatio != c.wantR {
			t.Errorf("EffectivePlanCoverage(%q/%v) = %q/%v, want %q/%v",
				c.mode, c.ratio, s.Mode, s.MinRatio, c.wantM, c.wantR)
		}
	}
}

func TestManagerEffectivePlanCoverageUnknownModeRejected(t *testing.T) {
	m := &Manager{cfg: Config{PlanCoverageCheck: "veto", PlanCoverageMinRatio: 0.8}}
	if _, err := m.EffectivePlanCoverage(); err == nil {
		t.Fatalf("EffectivePlanCoverage must reject unknown check mode \"veto\"")
	}
}

// TestPRDPlanCoverageStoredFieldRoundTrip proves the PRD's stored
// report serializes under the plan_coverage key with the settings,
// the counts, the unmatched list (verdict + spec fragment) and the
// acknowledgement state.
func TestPRDPlanCoverageStoredFieldRoundTrip(t *testing.T) {
	prd := &PRD{
		PlanCoverage: &PlanCoverage{
			Mode:     "block",
			MinRatio: 0.9,
			Report: PlanCoverageReport{
				Covered:  4,
				Missing:  1,
				Weakened: 1,
				Unmatched: []RequirementVerdict{
					{Requirement: "Add tests for the new endpoint", Verdict: CoverageMissing},
					{Requirement: "reports/daily.txt", Verdict: CoverageWeakened},
				},
			},
			Ack: &PlanCoverageAck{
				By:          "operator@dmz",
				Note:        "acknowledged for the demo PRD",
				Fingerprint: "fp-1234",
			},
		},
	}
	b, err := json.Marshal(prd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out PRD
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := out.PlanCoverage
	if got == nil {
		t.Fatalf("plan_coverage field lost in round-trip")
	}
	if got.Mode != "block" || got.MinRatio != 0.9 {
		t.Fatalf("settings = %q/%v, want block/0.9", got.Mode, got.MinRatio)
	}
	if got.Report.Covered != 4 || got.Report.Missing != 1 || got.Report.Weakened != 1 {
		t.Fatalf("counts = %+v, want 4/1/1", got.Report)
	}
	if len(got.Report.Unmatched) != 2 {
		t.Fatalf("unmatched = %+v, want 2 entries", got.Report.Unmatched)
	}
	if got.Report.Unmatched[0].Requirement != "Add tests for the new endpoint" ||
		got.Report.Unmatched[0].Verdict != CoverageMissing {
		t.Fatalf("unmatched[0] = %+v", got.Report.Unmatched[0])
	}
	if got.Ack == nil || got.Ack.By != "operator@dmz" ||
		got.Ack.Note != "acknowledged for the demo PRD" || got.Ack.Fingerprint != "fp-1234" {
		t.Fatalf("ack = %+v", got.Ack)
	}

	// A PRD with no coverage stored still round-trips as unset.
	var clean PRD
	clean.Spec = "just a spec"
	b, _ = json.Marshal(clean)
	out = PRD{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal clean: %v", err)
	}
	if out.PlanCoverage != nil {
		t.Fatalf("clean PRD should keep PlanCoverage nil, got %+v", out.PlanCoverage)
	}
}
