package usagecalc

import (
	"context"
	"strings"
	"testing"
	"time"
)

// noSpace strips all whitespace, so the assertions are not broken by the CLI
// rendering's 80-column wrapping.
func noSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

// TestScheduleValidAndApply covers the profile schedule guard: a schedule is valid only
// with a weekday in range and a parseable clock, and it overwrites the reset's weekday
// and clock while keeping the reset's zone.
func TestScheduleValidAndApply(t *testing.T) {
	res := ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "Europe/Berlin", ServerZone: true, FellBack: true}
	cases := []struct {
		name     string
		sched    Schedule
		valid    bool
		wantWD   time.Weekday
		wantClk  string
		wantZone string
	}{
		{"valid", Schedule{Weekday: time.Friday, Clock: "16:00"}, true, time.Friday, "16:00", "Europe/Berlin"},
		{"bad clock", Schedule{Weekday: time.Friday, Clock: "25:00"}, false, time.Sunday, "08:00", "Europe/Berlin"},
		{"empty clock", Schedule{Weekday: time.Friday, Clock: ""}, false, time.Sunday, "08:00", "Europe/Berlin"},
		{"bad weekday", Schedule{Weekday: time.Monday + 7, Clock: "16:00"}, false, time.Sunday, "08:00", "Europe/Berlin"},
		{"zero", Schedule{}, false, time.Sunday, "08:00", "Europe/Berlin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sched.Valid(); got != tc.valid {
				t.Fatalf("Valid: got %v; want %v", got, tc.valid)
			}
			got, applied := tc.sched.Apply(res)
			if applied != tc.valid {
				t.Fatalf("Apply applied: got %v; want %v", applied, tc.valid)
			}
			if got.Weekday != tc.wantWD || got.Clock != tc.wantClk || got.Zone != tc.wantZone {
				t.Errorf("Apply: got %v %q zone %q; want %v %q zone %q", got.Weekday, got.Clock, got.Zone, tc.wantWD, tc.wantClk, tc.wantZone)
			}
			if !got.ServerZone || !got.FellBack {
				t.Errorf("Apply: server-zone and fell-back markers must ride through, got %v %v", got.ServerZone, got.FellBack)
			}
		})
	}
}

// TestServiceProfileScheduleApplied: a valid profile schedule in the live settings
// replaces the calculator's weekday and clock for the advice, keeps the calculator's
// zone, and carries the reset_from_profile marker to the text rendering.
func TestServiceProfileScheduleApplied(t *testing.T) {
	f := newServiceFixture(t, Settings{
		Reset:    serviceReset, // Sunday 08:00 UTC
		Schedule: Schedule{Weekday: time.Wednesday, Clock: "16:00"},
	}, nil)
	f.seedReading(t)

	base, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if !base.Advice.ResetFromProfile {
		t.Errorf("ResetFromProfile: got false; want true")
	}
	if base.Advice.Zone != "UTC" {
		t.Errorf("Zone: got %q; want the calculator's zone UTC", base.Advice.Zone)
	}
	// The reading is Thursday 2026-10-22 08:00 UTC. Under the profile's
	// Wednesday 16:00 reset the week runs 2026-10-21 16:00 to 2026-10-28 16:00.
	wantNext := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)
	if base.Advice.NextResetAt == nil || !base.Advice.NextResetAt.Equal(wantNext) {
		t.Errorf("NextResetAt: got %v; want %v", base.Advice.NextResetAt, wantNext)
	}
	if noSpace(base.Text.CLI) == "" || !strings.Contains(noSpace(base.Text.CLI), noSpace("reset Wed 16:00 (from the claude-code billing profile)")) {
		t.Errorf("text: want the reset with the profile marker, got:\n%s", base.Text.CLI)
	}
}

// TestServiceProfileScheduleNotApplied: a schedule that does not validate leaves the
// calculator's schedule standing, with no profile marker.
func TestServiceProfileScheduleNotApplied(t *testing.T) {
	f := newServiceFixture(t, Settings{
		Reset:    serviceReset,
		Schedule: Schedule{Weekday: time.Friday, Clock: "25:00"},
	}, nil)
	f.seedReading(t)

	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Advice.ResetFromProfile {
		t.Errorf("ResetFromProfile: got true for an invalid schedule; want false")
	}
	// Sunday 08:00 reset: week runs 2026-10-18 08:00 to 2026-10-25 08:00.
	wantNext := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	if res.Advice.NextResetAt == nil || !res.Advice.NextResetAt.Equal(wantNext) {
		t.Errorf("NextResetAt: got %v; want %v", res.Advice.NextResetAt, wantNext)
	}
	if strings.Contains(res.Text.CLI, "billing profile") {
		t.Errorf("text: no profile marker expected, got:\n%s", res.Text.CLI)
	}
}

// TestServicePointValue: the live settings' point value rides into the advice and the
// text, marked as an estimate with its source and the profile's currency.
func TestServicePointValue(t *testing.T) {
	f := newServiceFixture(t, Settings{
		Reset: serviceReset,
		PointValue: &PointValue{
			Source:   PointValueSourceProfile,
			PerPoint: 0.2307692,
			PerWeek:  23.07692,
			Currency: "USD",
		},
	}, nil)
	f.seedReading(t)

	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	pv := res.Advice.PointValue
	if pv == nil {
		t.Fatal("PointValue: got nil; want the settings' value")
	}
	if !pv.Estimate {
		t.Errorf("Estimate: got false; want true (always an estimate)")
	}
	if pv.Source != PointValueSourceProfile {
		t.Errorf("Source: got %q; want %q", pv.Source, PointValueSourceProfile)
	}
	if pv.PerPoint != 0.2307692 || pv.PerWeek != 23.07692 || pv.Currency != "USD" {
		t.Errorf("figures: got %v %v %q; want the settings' figures", pv.PerPoint, pv.PerWeek, pv.Currency)
	}
	want := "point value (estimate, from the claude-code billing profile): 0.231 per point, 23.077 per week, USD"
	if !strings.Contains(noSpace(res.Text.CLI), noSpace(want)) {
		t.Errorf("text: want %q, got:\n%s", want, res.Text.CLI)
	}
}

// TestServicePointValueDefaultSource: a value from the assumed default profile carries
// the default source and the text names it as assumed.
func TestServicePointValueDefaultSource(t *testing.T) {
	f := newServiceFixture(t, Settings{
		Reset:      serviceReset,
		PointValue: &PointValue{Source: PointValueSourceDefault, PerPoint: 0.2, PerWeek: 20},
	}, nil)
	f.seedReading(t)

	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Advice.PointValue == nil || res.Advice.PointValue.Source != PointValueSourceDefault {
		t.Fatalf("PointValue: got %v; want the default source", res.Advice.PointValue)
	}
	if want := "point value (estimate, from the assumed default claude-code profile): 0.2 per point, 20 per week"; !strings.Contains(noSpace(res.Text.CLI), noSpace(want)) {
		t.Errorf("text: want %q, got:\n%s", want, res.Text.CLI)
	}
}

// TestServiceNoPointValue: settings without a point value leave the advice and the
// text without one.
func TestServiceNoPointValue(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)

	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Advice.PointValue != nil {
		t.Errorf("PointValue: got %v; want nil", res.Advice.PointValue)
	}
	if strings.Contains(res.Text.CLI, "point value") {
		t.Errorf("text: no point value line expected, got:\n%s", res.Text.CLI)
	}
}

// TestAdviseNormalizesPointValue: the point value in the input must name a known source
// and carry finite non-negative figures, or it does not ride into the advice; a known
// one is kept and forced into the estimate shape.
func TestAdviseNormalizesPointValue(t *testing.T) {
	in := Input{
		Readings: []Reading{{At: serviceReadingAt, Percent: &serviceReadingPct}},
		Reset:    serviceReset,
		Now:      serviceNow,
	}

	t.Run("unknown source dropped", func(t *testing.T) {
		got := Advise(Input{Readings: in.Readings, Reset: in.Reset, Now: in.Now,
			PointValue: &PointValue{Source: "ledger", PerPoint: 1, PerWeek: 100}})
		if got.PointValue != nil {
			t.Errorf("unknown source: got %v; want nil", got.PointValue)
		}
	})
	t.Run("empty source dropped", func(t *testing.T) {
		got := Advise(Input{Readings: in.Readings, Reset: in.Reset, Now: in.Now,
			PointValue: &PointValue{PerPoint: 1, PerWeek: 100}})
		if got.PointValue != nil {
			t.Errorf("empty source: got %v; want nil", got.PointValue)
		}
	})
	t.Run("negative figure dropped", func(t *testing.T) {
		got := Advise(Input{Readings: in.Readings, Reset: in.Reset, Now: in.Now,
			PointValue: &PointValue{Source: PointValueSourceProfile, PerPoint: -1, PerWeek: 100}})
		if got.PointValue != nil {
			t.Errorf("negative: got %v; want nil", got.PointValue)
		}
	})
	t.Run("kept and forced into estimate shape", func(t *testing.T) {
		got := Advise(Input{Readings: in.Readings, Reset: in.Reset, Now: in.Now,
			PointValue: &PointValue{Estimate: false, Source: PointValueSourceProfile, PerPoint: 0.2, PerWeek: 20, Currency: " usd "}})
		if got.PointValue == nil {
			t.Fatal("valid value: got nil; want kept")
		}
		if !got.PointValue.Estimate {
			t.Errorf("Estimate: got false; want true (forced)")
		}
		if got.PointValue.Currency != "USD" {
			t.Errorf("Currency: got %q; want USD", got.PointValue.Currency)
		}
	})
}
