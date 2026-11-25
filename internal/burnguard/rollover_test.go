package burnguard

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// rolloverReset is Sunday 07:00 in New York; 2026-11-29 07:00 EST is a reset.
var rolloverReset = usagecalc.ResetSpec{Weekday: time.Sunday, Clock: "07:00", Zone: "America/New_York"}

// rolloverData is one reading of 91% from the day before the reset, an idle bucket before
// the reset and cwMillion million cache-write tokens in the hour after it.
func rolloverData(t *testing.T, cwMillion int64) (now time.Time, rs []usagecalc.Reading, b []Bucket) {
	t.Helper()
	loc := ny(t)
	reset := time.Date(2026, 11, 29, 7, 0, 0, 0, loc)
	pct := 91.0
	rs = []usagecalc.Reading{{ID: "last-week", At: reset.Add(-12 * time.Hour), Percent: &pct}}
	b = []Bucket{
		{Hour: reset.Add(-24 * time.Hour).UTC(), Other: Tokens{CacheWrite: 1_000_000}},
		{Hour: reset.UTC(), Other: Tokens{CacheWrite: cwMillion * 1_000_000}},
	}
	return reset.Add(2 * time.Hour), rs, b
}

// TestProjectEstimatesTheNewWeekFromTokens: with only last week's reading, the projection
// starts the new week at 0 and adds the tokens since the reset.
func TestProjectEstimatesTheNewWeekFromTokens(t *testing.T) {
	now, rs, b := rolloverData(t, 20)
	p := Project(ProjectInput{Readings: rs, Reset: rolloverReset, Now: now, Buckets: b, Through: now, Coef: DefaultCoefficients})
	if p.Needs != "" {
		t.Fatalf("needs %q after the reset: there is no weekly projection", p.Needs)
	}
	want := 20 * DefaultCoefficients.CacheWritePerM // the tokens before the reset do not count
	if p.PercentAtReading != 0 || math.Abs(p.EstimatedNow-want) > 1e-6 {
		t.Fatalf("base %.2f, estimate %.2f; want base 0 and %.2f", p.PercentAtReading, p.EstimatedNow, want)
	}
}

// TestEstimatedProjectionHoldsWhenOverAndNotWhenUnder: the estimate is marked, carries the
// verdict, and the guard-to-advice gap is zero so the gap check cannot hide an over.
func TestEstimatedProjectionHoldsWhenOverAndNotWhenUnder(t *testing.T) {
	for _, c := range []struct {
		name string
		cw   int64
		over bool
	}{{"over", 20, true}, {"under", 0, false}} {
		now, rs, b := rolloverData(t, c.cw)
		p := Project(ProjectInput{Readings: rs, Reset: rolloverReset, Now: now, Buckets: b, Through: now, Coef: DefaultCoefficients})
		if p.Needs != "" || !p.Estimated || p.Over != c.over || p.AdviceProjected != p.Projected {
			t.Fatalf("%s: %+v", c.name, p)
		}
		if reset := time.Date(2026, 11, 29, 7, 0, 0, 0, ny(t)); !p.ReadAt.Equal(reset) || !p.NextReset.Equal(reset.AddDate(0, 0, 7)) {
			t.Fatalf("%s: base %v, next %v", c.name, p.ReadAt, p.NextReset)
		}
		if c.over && !strings.Contains(numbers(p, false), "estimated from tokens since the weekly reset") {
			t.Fatalf("alert numbers do not say estimated: %s", numbers(p, false))
		}
	}
}

// TestNoTokenDataGivesNoEstimate: with no token data or no prices there is nothing to
// estimate from; a zero would read as an unused week.
func TestNoTokenDataGivesNoEstimate(t *testing.T) {
	now, rs, b := rolloverData(t, 20)
	for name, in := range map[string]ProjectInput{
		"no buckets": {Readings: rs, Reset: rolloverReset, Now: now, Coef: DefaultCoefficients},
		"no prices":  {Readings: rs, Reset: rolloverReset, Now: now, Buckets: b, Through: now},
	} {
		if p := Project(in); p.Needs != usagecalc.NeedsReading || p.Estimated {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}

// TestReadingAfterTheResetReplacesTheEstimate: the first reading of the new week is the
// base again.
func TestReadingAfterTheResetReplacesTheEstimate(t *testing.T) {
	now, rs, b := rolloverData(t, 20)
	pct := 3.0
	rs = append(rs, usagecalc.Reading{ID: "new-week", At: now.Add(-time.Hour), Percent: &pct})
	p := Project(ProjectInput{Readings: rs, Reset: rolloverReset, Now: now, Buckets: b, Through: now, Coef: DefaultCoefficients})
	if p.Needs != "" || p.Estimated || p.PercentAtReading != pct {
		t.Fatalf("%+v", p)
	}
}

// TestEstimateUsesTheCalibrationOfTheCoveredReadings: last week's readings that the token
// data reaches back to scale the estimate; older ones, with no tokens behind them, do not.
func TestEstimateUsesTheCalibrationOfTheCoveredReadings(t *testing.T) {
	reset := time.Date(2026, 11, 29, 7, 0, 0, 0, ny(t))
	reading := func(hoursBefore int, pct float64) usagecalc.Reading {
		return usagecalc.Reading{At: reset.Add(-time.Duration(hoursBefore) * time.Hour), Percent: &pct}
	}
	bucket := func(hoursBefore int, cw int64) Bucket {
		return Bucket{Hour: reset.Add(-time.Duration(hoursBefore) * time.Hour).UTC(), Other: Tokens{CacheWrite: cw}}
	}
	// 10M tokens are priced at 3.73 points an interval; the readings moved 1.5 times that.
	step := 1.5 * 10 * DefaultCoefficients.CacheWritePerM
	rs := []usagecalc.Reading{
		reading(400, 1), reading(399, 50), // before the token data: ignored
		reading(30, 60), reading(29, 60+step), reading(28, 60+2*step),
	}
	b := []Bucket{bucket(30, 10_000_000), bucket(29, 10_000_000), bucket(0, 10_000_000)}
	est, ratio, ok := EstimateWeek(ProjectInput{Readings: rs, Reset: rolloverReset, Now: reset.Add(2 * time.Hour),
		Buckets: b, Coef: DefaultCoefficients})
	if !ok || math.Abs(ratio-1.5) > 1e-6 || math.Abs(est.Points-step) > 1e-6 {
		t.Fatalf("ok %v ratio %.3f points %.3f, want ratio 1.5 and %.3f", ok, ratio, est.Points, step)
	}
}

// TestEvaluatePaceEstimatedWeek: an estimated week is decided on, says so, and is not
// counted as a reading's age.
func TestEvaluatePaceEstimatedWeek(t *testing.T) {
	in := paceInput(PaceModeOn)
	in.WeekKnown, in.WeekReset, in.WeekFromTokens = true, true, true
	in.WeekReadAt, in.WeekPercent, in.WeekPointsSince, in.LineNow = time.Time{}, 0, 9, 1
	in.WindowKnown = false
	d := EvaluatePace(in)
	if !d.Hold || d.Rule != PaceRuleWeek || !d.WeekFromTokens || d.Stale || d.ReadingAgeMinutes != 0 {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(d.Reason, "week reset") || !strings.Contains(d.Reason, "estimated from tokens since the reset") {
		t.Fatalf("reason %q", d.Reason)
	}
}
