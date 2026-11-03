// TestUsageCalculatorSettings*: the billing-profile inputs the settings mapper adds —
// the plan's reset schedule (valid, invalid, and partial) and the point value
// (operator profile, assumed default, currency normalization), on top of the
// calculator's own reset zone.

package server

import (
	"math"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// calcSettingsWith is UsageCalculatorSettings over a calculator config with the reset
// zone pinned to UTC (so the zone resolution is deterministic), plus the given
// claude-code billing profile.
func calcSettingsWith(t *testing.T, p config.BillingProfile, withBilling bool) config.UsageConfig {
	t.Helper()
	u := config.UsageConfig{
		Calculator: config.UsageCalculatorConfig{
			ResetWeekday: "sunday",
			ResetTime:    "08:00",
			ResetZone:    "UTC",
		},
	}
	if withBilling {
		u.Billing = map[string]config.BillingProfile{"claude-code": p}
	}
	return u
}

func TestUsageCalculatorSettingsProfileScheduleApplied(t *testing.T) {
	u := calcSettingsWith(t, config.BillingProfile{
		Mode:         config.BillingModeBundledPlan,
		MonthlyPrice: 100,
		Currency:     "USD",
		ResetWeekday: "FRIDAY",
		ResetTime:    " 16:00 ",
	}, true)

	s := UsageCalculatorSettings(u)
	if s.Reset.Weekday != time.Friday || s.Reset.Clock != "16:00" {
		t.Errorf("reset: got %v %q; want the profile's Friday 16:00", s.Reset.Weekday, s.Reset.Clock)
	}
	if s.Reset.Zone != "UTC" {
		t.Errorf("zone: got %q; want the calculator's zone UTC", s.Reset.Zone)
	}
	if s.Schedule != (usagecalc.Schedule{Weekday: time.Friday, Clock: "16:00"}) {
		t.Errorf("Schedule: got %+v; want the profile's provenance marker", s.Schedule)
	}
	if s.PointValue == nil {
		t.Fatal("PointValue: got nil; want the profile's value")
	}
	// 100 a month over 52/12 weeks, a point being 1/100 of a week.
	wantPerPoint := 100.0 / (52.0 / 12.0) / 100.0
	if math.Abs(s.PointValue.PerPoint-wantPerPoint) > 1e-9 {
		t.Errorf("PerPoint: got %v; want %v", s.PointValue.PerPoint, wantPerPoint)
	}
	wantPerWeek := wantPerPoint * usagecalc.WeeklyAllowance
	if math.Abs(s.PointValue.PerWeek-wantPerWeek) > 1e-9 {
		t.Errorf("PerWeek: got %v; want %v", s.PointValue.PerWeek, wantPerWeek)
	}
	if !s.PointValue.Estimate {
		t.Errorf("Estimate: got false; want true")
	}
	if s.PointValue.Source != usagecalc.PointValueSourceProfile {
		t.Errorf("Source: got %q; want %q", s.PointValue.Source, usagecalc.PointValueSourceProfile)
	}
}

func TestUsageCalculatorSettingsDefaultProfile(t *testing.T) {
	// No billing profiles at all: the assumed default claude-code profile stands in.
	s := UsageCalculatorSettings(config.UsageConfig{})

	if s.Reset.Weekday != time.Sunday || s.Reset.Clock != "08:00" {
		t.Errorf("reset: got %v %q; want the calculator's Sunday 08:00", s.Reset.Weekday, s.Reset.Clock)
	}
	if s.Schedule != (usagecalc.Schedule{}) {
		t.Errorf("Schedule: got %+v; want zero (the default profile names none)", s.Schedule)
	}
	pv := s.PointValue
	if pv == nil {
		t.Fatal("PointValue: got nil; want the default profile's (zero) value")
	}
	if pv.Source != usagecalc.PointValueSourceDefault {
		t.Errorf("Source: got %q; want %q", pv.Source, usagecalc.PointValueSourceDefault)
	}
	if pv.PerPoint != 0 || pv.PerWeek != 0 || !pv.Estimate {
		t.Errorf("figures: got %v %v %v; want zero figures, still marked an estimate", pv.PerPoint, pv.PerWeek, pv.Estimate)
	}
}

func TestUsageCalculatorSettingsInvalidProfileScheduleIgnored(t *testing.T) {
	cases := []struct {
		name string
		p    config.BillingProfile
	}{
		{"bad time", config.BillingProfile{Mode: config.BillingModeBundledPlan, MonthlyPrice: 100, ResetWeekday: "friday", ResetTime: "25:00"}},
		{"time only", config.BillingProfile{Mode: config.BillingModeBundledPlan, MonthlyPrice: 100, ResetTime: "16:00"}},
		{"weekday only", config.BillingProfile{Mode: config.BillingModeBundledPlan, MonthlyPrice: 100, ResetWeekday: "friday"}},
		{"unknown weekday", config.BillingProfile{Mode: config.BillingModeBundledPlan, MonthlyPrice: 100, ResetWeekday: "solstice", ResetTime: "16:00"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := UsageCalculatorSettings(calcSettingsWith(t, tc.p, true))
			if s.Schedule != (usagecalc.Schedule{}) {
				t.Errorf("Schedule: got %+v; want zero for the invalid/partial schedule", s.Schedule)
			}
			if s.Reset.Weekday != time.Sunday || s.Reset.Clock != "08:00" {
				t.Errorf("reset: got %v %q; want the calculator's Sunday 08:00 standing", s.Reset.Weekday, s.Reset.Clock)
			}
			// The point value is unaffected by the schedule.
			if s.PointValue == nil || s.PointValue.Source != usagecalc.PointValueSourceProfile {
				t.Errorf("PointValue: got %+v; want the operator profile's value", s.PointValue)
			}
			if s.PointValue == nil || math.Abs(s.PointValue.PerPoint-100.0/(52.0/12.0)/100.0) > 1e-9 {
				t.Errorf("PerPoint: got %v; want the profile's", s.PointValue)
			}
		})
	}
}
