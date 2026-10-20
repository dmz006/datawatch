// plan_coverage_test.go — settings normalization + the
// marshal/unmarshal round-trip guarantee for the two new Autonomous
// config keys. The round-trip test encodes the documented gotcha in
// config.go: no field whose explicit clear value must survive
// handleAutonomousConfig's json.Marshal + json.Unmarshal pass may use
// omitempty.
package config

import (
	"encoding/json"
	"testing"
)

// TestPlanCoverageDefaultSettings: a config constructed with the two
// keys unset behaves as check mode "block" and min ratio 1.0 — the
// safe defaults.
func TestPlanCoverageDefaultSettings(t *testing.T) {
	var a AutonomousConfig // both keys at their zero values
	s, err := a.EffectivePlanCoverage()
	if err != nil {
		t.Fatalf("EffectivePlanCoverage with unset keys: %v", err)
	}
	if s.Mode != PlanCoverageBlock {
		t.Fatalf("unset check mode = %q, want %q", s.Mode, PlanCoverageBlock)
	}
	if s.MinRatio != 1.0 {
		t.Fatalf("unset min ratio = %v, want 1.0", s.MinRatio)
	}
	if _, err := EffectivePlanCoverage("", 0); err != nil {
		t.Fatalf("EffectivePlanCoverage(\"\", 0): %v", err)
	}
}

func TestNormalizePlanCoverageCheck(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", PlanCoverageBlock, false}, // unset -> safe default
		{"block", "block", false},
		{"warn", "warn", false},
		{"off", "off", false},
		{"OFF", "off", false},     // case-insensitive
		{" warn ", "warn", false}, // trimmed
		{"Block", "block", false},
		{"strict", "", true}, // unknown -> rejected, not guessed
		{"blockk", "", true}, // typo -> rejected
		{"BLOCK ALL", "", true},
	}
	for _, c := range cases {
		got, err := NormalizePlanCoverageCheck(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizePlanCoverageCheck(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePlanCoverageCheck(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizePlanCoverageCheck(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePlanCoverageRatio(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 1.0}, // zero/unset -> full coverage required
		{-0.25, 1.0},
		{0.9, 0.9}, // explicit fractional ratio passes through
		{0.5, 0.5},
		{1.0, 1.0}, // upper bound is allowed
		{1.5, 1.0}, // clamped into (0,1]
		{2.5, 1.0},
	}
	for _, c := range cases {
		if got := NormalizePlanCoverageRatio(c.in); got != c.want {
			t.Errorf("NormalizePlanCoverageRatio(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestPlanCoverageSettingsRoundTrip proves the no-omitempty guarantee:
// an explicit "off" check mode and an explicit 0.9 min ratio both
// survive a json.Marshal + json.Unmarshal round-trip (the exact path
// handleAutonomousConfig uses to persist the manager's live config),
// as does a ratio explicitly cleared to 0.
func TestPlanCoverageSettingsRoundTrip(t *testing.T) {
	orig := AutonomousConfig{
		PollIntervalSeconds:  45, // an omitempty field, populated
		PlanCoverageCheck:    "off",
		PlanCoverageMinRatio: 0.9,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AutonomousConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PlanCoverageCheck != "off" {
		t.Fatalf("check mode after round-trip = %q, want \"off\" (omitempty would have elided/mis-merged this)", out.PlanCoverageCheck)
	}
	if out.PlanCoverageMinRatio != 0.9 {
		t.Fatalf("min ratio after round-trip = %v, want 0.9", out.PlanCoverageMinRatio)
	}
	if got, err := out.EffectivePlanCoverage(); err != nil || got.Mode != "off" || got.MinRatio != 0.9 {
		t.Fatalf("effective settings after round-trip = %+v (err %v), want off/0.9", got, err)
	}

	// A change to the explicit clear value must also round-trip.
	orig.PlanCoverageCheck = "block"
	orig.PlanCoverageMinRatio = 0
	b, _ = json.Marshal(orig)
	out = AutonomousConfig{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal 2: %v", err)
	}
	if out.PlanCoverageCheck != "block" || out.PlanCoverageMinRatio != 0 {
		t.Fatalf("second round-trip = %q/%v, want block/0", out.PlanCoverageCheck, out.PlanCoverageMinRatio)
	}
	if s, err := out.EffectivePlanCoverage(); err != nil || s.MinRatio != 1.0 {
		t.Fatalf("effective from explicit 0 = %+v (err %v), want ratio 1.0", s, err)
	}
}

// TestPlanCoverageUnknownModeRejectedAtEffectiveLevel: EffectivePlanCoverage
// (the consumer entry point) rejects unknown modes rather than
// defaulting them.
func TestPlanCoverageUnknownModeRejectedAtEffectiveLevel(t *testing.T) {
	var a AutonomousConfig
	a.PlanCoverageCheck = "reject-everything"
	if _, err := a.EffectivePlanCoverage(); err == nil {
		t.Fatalf("EffectivePlanCoverage with unknown mode must return an error")
	}
}
