package usagecalc

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// TestAdviseNeverReportsLastWeeksPercentAsCurrent: once a reset has passed since the newest
// reading, its percentage is not this week's and must not be reported as current.
func TestAdviseNeverReportsLastWeeksPercentAsCurrent(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	reset := time.Date(2026, 11, 29, 7, 0, 0, 0, loc)
	pct := 91.0
	a := Advise(Input{
		Readings: []Reading{{At: reset.Add(-12 * time.Hour), Percent: &pct}},
		Reset:    ResetSpec{Weekday: time.Sunday, Clock: "07:00", Zone: "America/New_York"},
		Now:      reset.Add(6 * time.Hour),
	})
	if a.Percent == pct {
		t.Fatalf("advice reports last week's %.0f%% as current: needs %q, headline %q", a.Percent, a.Needs, a.Advice.Headline)
	}
	if a.NextResetAt == nil || !a.NextResetAt.Equal(reset.AddDate(0, 0, 7)) {
		t.Fatalf("next reset = %v, want the reset a week after %v", a.NextResetAt, reset)
	}
}

// rolledInput is a 91% reading twelve hours before the reset of 2026-11-29 07:00 New York,
// read six hours after it.
func rolledInput(t *testing.T) (Input, time.Time) {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	reset := time.Date(2026, 11, 29, 7, 0, 0, 0, loc)
	pct, session := 91.0, 40.0
	return Input{
		Readings: []Reading{{At: reset.Add(-12 * time.Hour), Percent: &pct, SessionPercent: &session}},
		Reset:    ResetSpec{Weekday: time.Sunday, Clock: "07:00", Zone: "America/New_York"},
		Now:      reset.Add(6 * time.Hour),
	}, reset
}

// TestAdviseEstimatesTheNewWeek: the week's figures are measured from now, from 0 at the
// reset, and marked as an estimate.
func TestAdviseEstimatesTheNewWeek(t *testing.T) {
	in, reset := rolledInput(t)
	in.SinceReset = &WeekEstimate{Points: 4, PointsPerHour: 0.5}
	a := Advise(in)
	if a.Needs != "" || !a.Estimated || a.EstimateNote != EstimateNoteText || a.Percent != 4 {
		t.Fatalf("needs %q estimated %v percent %v", a.Needs, a.Estimated, a.Percent)
	}
	if math.Abs(a.DaysSinceReset-0.25) > 1e-9 || math.Abs(a.DaysLeft-6.75) > 1e-9 || math.Abs(a.PaceLine-0.25/7*100) > 1e-9 {
		t.Fatalf("days since %v, left %v, pace line %v", a.DaysSinceReset, a.DaysLeft, a.PaceLine)
	}
	if !a.LastResetAt.Equal(reset) || !a.ReadAt.Equal(in.Readings[0].At) {
		t.Fatalf("last reset %v, read at %v (the reading keeps its own time)", a.LastResetAt, a.ReadAt)
	}
	if a.RecentBurnPer24h != 12 || a.WeekAvgBurnPer24h != 16 || math.Abs(a.ProjectedEndRecent-85) > 1e-9 {
		t.Fatalf("burn %v/%v projected %v", a.RecentBurnPer24h, a.WeekAvgBurnPer24h, a.ProjectedEndRecent)
	}
	if a.Session != nil || len(a.Series) != 0 || len(a.Daily) != 0 {
		t.Fatalf("last week's data shown: session %v series %v daily %v", a.Session, a.Series, a.Daily)
	}
	text := FormatCLI(AdviceResult{Advice: a, Stale: true, StaleReason: ResetPassedReason})
	flat := strings.Join(strings.Fields(text), " ")
	for _, want := range []string{"about 4% estimated", "note: " + EstimateNoteText, "stale: " + ResetPassedReason} {
		if !strings.Contains(flat, want) {
			t.Errorf("text is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(flat, "last copy") || strings.Contains(flat, "91%") {
		t.Errorf("text names a copy or last week's percent:\n%s", text)
	}
}

// TestAdviseWithNoEstimateNeedsAReading: without token data the advice asks for a reading
// and says why.
func TestAdviseWithNoEstimateNeedsAReading(t *testing.T) {
	in, _ := rolledInput(t)
	a := Advise(in)
	if a.Needs != NeedsReading || a.Percent != 0 || a.ReadAt != nil || !strings.Contains(a.Advice.Headline, "weekly reset passed") {
		t.Fatalf("%+v", a)
	}
	if text := FormatCLI(AdviceResult{Advice: a}); !strings.Contains(text, "no reading since the weekly reset") {
		t.Errorf("text:\n%s", text)
	}
}

// TestResetPassedAcrossADaylightSavingWeek: the week of the autumn clock change is 169
// hours long; the reset is the local clock time, not 168 hours after the last one.
func TestResetPassedAcrossADaylightSavingWeek(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	spec := ResetSpec{Weekday: time.Sunday, Clock: "07:00", Zone: "America/New_York"}
	pct := 91.0
	rs := []Reading{{At: time.Date(2026, 10, 30, 12, 0, 0, 0, loc), Percent: &pct}}
	start := time.Date(2026, 10, 25, 7, 0, 0, 0, loc)
	if ResetPassed(rs, spec, start.Add(168*time.Hour)) {
		t.Fatal("168 hours in, the 169 hour week has not reset")
	}
	if !ResetPassed(rs, spec, start.Add(169*time.Hour)) {
		t.Fatal("the reset at 07:00 local did not count")
	}
	a := Advise(Input{Readings: rs, Reset: spec, Now: start.Add(169 * time.Hour), SinceReset: &WeekEstimate{}})
	if !a.Estimated || a.Percent != 0 || a.DaysSinceReset != 0 || math.Abs(a.DaysLeft-7) > 1e-9 {
		t.Fatalf("at the reset: %+v", a)
	}
}

// TestServiceMarksARolledOverAdviceStale: the service asks its estimator only once a reset
// has passed, and marks the advice stale with the plain reason.
func TestServiceMarksARolledOverAdviceStale(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)
	calls := 0
	WithWeekEstimator(func(_ context.Context, rs []Reading, reset ResetSpec, now time.Time) *WeekEstimate {
		calls++
		return &WeekEstimate{Points: 2, PointsPerHour: 0.5}
	})(f.svc)
	ctx := context.Background()

	res, err := f.svc.Advice(ctx, Options{})
	if err != nil || calls != 0 || res.Stale || res.Advice.Estimated {
		t.Fatalf("before the reset: calls %d stale %v err %v", calls, res.Stale, err)
	}
	f.svc.now = func() time.Time { return serviceNow.Add(8 * time.Hour) }
	res, err = f.svc.Advice(ctx, Options{})
	if err != nil || calls != 1 || !res.Stale || res.StaleReason != ResetPassedReason || !res.Advice.Estimated || res.Advice.Percent != 2 {
		t.Fatalf("after the reset: calls %d %+v err %v", calls, res, err)
	}
}
