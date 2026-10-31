package usagecalc

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

// Shared helpers for the package's tests.

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }

func newYork(t testing.TB) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return loc
}

func sundayEight() ResetSpec {
	return ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "America/New_York"}
}

func at(t testing.TB, y int, m time.Month, d, h, min int) time.Time {
	t.Helper()
	return time.Date(y, m, d, h, min, 0, 0, newYork(t))
}

func percentReading(at time.Time, percent float64) Reading {
	return Reading{ID: at.Format(time.RFC3339), At: at, Percent: f64(percent), Source: "typed"}
}

// workedExample is the 22:08 reading of Friday 2026-10-23: 72 percent, 67 percent at 17:09,
// reset Sunday 2026-10-18 08:00 New York, three lanes, baseline 3, safety 5, typed work 20.
func workedExample(t testing.TB) Input {
	return Input{
		Readings: []Reading{
			percentReading(at(t, 2026, 10, 23, 17, 9), 67),
			percentReading(at(t, 2026, 10, 23, 22, 8), 72),
		},
		Reset:          sundayEight(),
		Now:            at(t, 2026, 10, 23, 23, 8),
		Lanes:          3,
		MaxLanes:       3,
		BaselinePer24h: f64(3),
		SafetyPoints:   f64(5),
		RemainingWork:  RemainingWork{Typed: f64(20)},
	}
}

func near(t testing.TB, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (±%v)", name, got, want, tol)
	}
}

// onePlace asserts a figure at the one-decimal precision the screens show.
func onePlace(t testing.TB, name string, got, want float64) {
	t.Helper()
	if round1(got) != want {
		t.Errorf("%s = %v (rounds to %v), want %v", name, got, round1(got), want)
	}
}

func clock(t testing.TB, when *time.Time) string {
	t.Helper()
	if when == nil {
		return "none"
	}
	return when.In(newYork(t)).Format("Mon 15:04")
}

func TestAdviseWorkedExample(t *testing.T) {
	in := workedExample(t)
	a := Advise(in)
	if a.Needs != "" {
		t.Fatalf("Needs = %q, want none", a.Needs)
	}
	onePlace(t, "percent", a.Percent, 72)
	near(t, "days since reset", a.DaysSinceReset, 5.59, 0.005)
	near(t, "days left", a.DaysLeft, 1.41, 0.005)
	onePlace(t, "pace line", a.PaceLine, 79.8)
	onePlace(t, "headroom", a.Headroom, 7.8)
	onePlace(t, "allowance per 24h", a.AllowancePer24h, 14.3)
	onePlace(t, "recent burn", a.RecentBurnPer24h, 24.1)
	onePlace(t, "week average burn", a.WeekAvgBurnPer24h, 12.9)
	onePlace(t, "projected end recent", a.ProjectedEndRecent, 106.0)
	onePlace(t, "projected end average", a.ProjectedEndAvg, 90.2)
	if a.Verdict != VerdictOver || a.VerdictAvg != VerdictWithin || a.AheadOfPace {
		t.Errorf("verdicts = %s/%s ahead=%v, want over/within/false", a.Verdict, a.VerdictAvg, a.AheadOfPace)
	}
	if got := clock(t, a.RunOutAtRecent); got != "Sun 02:02" {
		t.Errorf("run out recent = %s, want Sun 02:02", got)
	}
	near(t, "hours early recent", a.HoursEarlyRecent, 6, 0.1)
	if a.RunOutAtAvg != nil || a.HoursEarlyAvg != 0 {
		t.Errorf("run out at the week average = %v/%v, want none", a.RunOutAtAvg, a.HoursEarlyAvg)
	}
	if a.RecentFallback || a.FallbackReason != "" || a.FallbackNotice != "" {
		t.Errorf("unexpected fallback %v %q %q", a.RecentFallback, a.FallbackReason, a.FallbackNotice)
	}
	if a.Ceiling != 100 || a.ExtraCreditPoints != 0 {
		t.Errorf("ceiling/extra = %v/%v, want 100/0", a.Ceiling, a.ExtraCreditPoints)
	}

	onePlace(t, "per lane", a.PerLanePer24h, 7.0)
	onePlace(t, "baseline", a.BaselinePer24h, 3)
	burn := []float64{3.0, 10.0, 17.1, 24.1}
	end := []float64{76.2, 86.1, 96.1, 106.0}
	if len(a.ByLanes) != 4 {
		t.Fatalf("by_lanes has %d rows, want 4", len(a.ByLanes))
	}
	for i, row := range a.ByLanes {
		if row.Lanes != i {
			t.Errorf("row %d lanes = %d", i, row.Lanes)
		}
		onePlace(t, "row burn", row.BurnPer24h, burn[i])
		onePlace(t, "row projected end", row.ProjectedEnd, end[i])
		if (row.RunOutAt != nil) != (i == 3) {
			t.Errorf("row %d run out = %v", i, row.RunOutAt)
		}
	}
	if a.Chosen == nil || a.Chosen.Lanes != 3 {
		t.Errorf("chosen = %+v, want the 3-lane row", a.Chosen)
	}
	if got := clock(t, a.ByLanes[3].RunOutAt); got != "Sun 02:02" {
		t.Errorf("3-lane run out = %s", got)
	}

	// The three headlines, word for word from the design.
	want := map[string]struct {
		lanes    int
		headline string
	}{
		PolicyUseAll:      {2, "Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%."},
		PolicyPace:        {1, "Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%."},
		PolicyFinishEarly: {3, "Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare."},
	}
	for policy, w := range want {
		in.Policy = policy
		got := Advise(in).Advice
		if got.Policy != policy || got.LanesSuggested != w.lanes || got.Headline != w.headline {
			t.Errorf("%s: got %d lanes %q, want %d lanes %q", policy, got.LanesSuggested, got.Headline, w.lanes, w.headline)
		}
	}

	in.Policy = PolicyFinishEarly
	f := Advise(in).Advice.Finish
	if f == nil || f.NoBurn || !f.BeforeAllowance {
		t.Fatalf("finish = %+v", f)
	}
	near(t, "finish hours", *f.Hours, 22.8, 0.05)
	near(t, "finish end percent", *f.EndPercent, 94.8, 0.05)
	near(t, "finish spare", *f.SparePoints, 5.2, 0.05)
	if got := clock(t, f.At); got != "Sat 20:54" {
		t.Errorf("finish at = %s, want Sat 20:54", got)
	}
}

func TestAdviseWorkedExampleSeriesAndSession(t *testing.T) {
	in := workedExample(t)
	in.Readings[1].SessionPercent = f64(15)
	a := Advise(in)
	if a.Session == nil || a.Session.Percent != 15 || a.Session.Label != "secondary" || !a.Session.ReadAt.Equal(in.Readings[1].At) {
		t.Errorf("session = %+v", a.Session)
	}
	if len(a.Series) != 2 || a.Series[1].Percent != 72 {
		t.Fatalf("series = %+v", a.Series)
	}
	near(t, "series hours since reset", a.Series[1].HoursSinceReset, 134.133, 0.01)
	near(t, "latest age", *a.LatestAgeHours, 1, 1e-9)

	// The session percentage feeds nothing: removing it changes no number.
	in.Readings[1].SessionPercent = nil
	b := Advise(in)
	if b.Session != nil {
		t.Errorf("session = %+v, want nil", b.Session)
	}
	a.Session = nil
	if !reflect.DeepEqual(a, b) {
		t.Errorf("session percent changed a calculation")
	}
}

func TestAdviseLatestAgeHours(t *testing.T) {
	in := workedExample(t)
	in.Now = time.Time{}
	if a := Advise(in); a.LatestAgeHours != nil {
		t.Errorf("age without Now = %v, want nil", *a.LatestAgeHours)
	}
	in.Now = in.Readings[1].At.Add(-time.Hour)
	if a := Advise(in); a.LatestAgeHours == nil || *a.LatestAgeHours != 0 {
		t.Errorf("age with Now before the reading = %v, want 0", a.LatestAgeHours)
	}
}

func TestAdviseRecentFallback(t *testing.T) {
	notice := "No earlier reading at least 2 hours back; the week average is used, so this figure is smoother than the recent burn."
	if FallbackNoticeText != notice {
		t.Fatalf("notice = %q", FallbackNoticeText)
	}
	base := func(readings ...Reading) Input {
		return Input{Readings: readings, Reset: sundayEight(), Lanes: 3, RecentLanes: intp(3)}
	}

	t.Run("no previous reading", func(t *testing.T) {
		a := Advise(base(percentReading(at(t, 2026, 10, 18, 10, 20), 2)))
		if !a.RecentFallback || a.FallbackReason != "no_previous" || a.FallbackNotice != notice {
			t.Errorf("fallback = %v %q %q", a.RecentFallback, a.FallbackReason, a.FallbackNotice)
		}
		if a.RecentBurnPer24h != a.WeekAvgBurnPer24h {
			t.Errorf("recent %v != week average %v", a.RecentBurnPer24h, a.WeekAvgBurnPer24h)
		}
	})
	t.Run("gap of 1.17 hours", func(t *testing.T) {
		a := Advise(base(
			percentReading(at(t, 2026, 10, 21, 18, 30), 33),
			percentReading(at(t, 2026, 10, 21, 19, 40), 34),
		))
		if !a.RecentFallback || a.FallbackReason != "gap_under_2h" || a.FallbackNotice != notice {
			t.Errorf("fallback = %v %q %q", a.RecentFallback, a.FallbackReason, a.FallbackNotice)
		}
		near(t, "recent burn", a.RecentBurnPer24h, a.WeekAvgBurnPer24h, 1e-12)
	})
	t.Run("gap of exactly 2 hours", func(t *testing.T) {
		a := Advise(base(
			percentReading(at(t, 2026, 10, 21, 17, 40), 32),
			percentReading(at(t, 2026, 10, 21, 19, 40), 34),
		))
		if a.RecentFallback || a.FallbackReason != "" || a.FallbackNotice != "" {
			t.Errorf("fallback = %v %q %q, want none", a.RecentFallback, a.FallbackReason, a.FallbackNotice)
		}
		near(t, "recent burn", a.RecentBurnPer24h, 24, 1e-9)
	})
	t.Run("previous reading from the week before", func(t *testing.T) {
		a := Advise(base(
			percentReading(at(t, 2026, 10, 17, 12, 0), 90),
			percentReading(at(t, 2026, 10, 19, 12, 0), 10),
		))
		if !a.RecentFallback || a.FallbackReason != "no_previous" {
			t.Errorf("fallback = %v %q, want no_previous", a.RecentFallback, a.FallbackReason)
		}
	})
	t.Run("a spend reading in between is skipped", func(t *testing.T) {
		spend := Reading{ID: "s", At: at(t, 2026, 10, 21, 18, 40), SpendUSD: f64(12)}
		a := Advise(base(
			percentReading(at(t, 2026, 10, 21, 14, 40), 30),
			spend,
			percentReading(at(t, 2026, 10, 21, 19, 40), 34),
		))
		if a.RecentFallback {
			t.Errorf("fallback with a percentage reading 5 hours back")
		}
		near(t, "recent burn", a.RecentBurnPer24h, 4.0/5*24, 1e-9)
	})
	t.Run("a falling percentage never gives a negative burn", func(t *testing.T) {
		a := Advise(base(
			percentReading(at(t, 2026, 10, 21, 10, 0), 40),
			percentReading(at(t, 2026, 10, 21, 20, 0), 38),
		))
		if a.RecentBurnPer24h != 0 {
			t.Errorf("recent burn = %v, want 0", a.RecentBurnPer24h)
		}
	})
}

func TestAdviseExtraCreditCeiling(t *testing.T) {
	in := workedExample(t)
	baseline := Advise(in)

	in.ExtraCreditPoints = 10
	a := Advise(in)
	if a.Ceiling != 110 || a.ExtraCreditPoints != 10 {
		t.Fatalf("ceiling/extra = %v/%v, want 110/10", a.Ceiling, a.ExtraCreditPoints)
	}
	onePlace(t, "projected end", a.ProjectedEndRecent, 106.0)
	if a.Verdict != VerdictWithin {
		t.Errorf("verdict = %s, want within at ceiling 110", a.Verdict)
	}
	if a.RunOutAtRecent != nil || a.HoursEarlyRecent != 0 {
		t.Errorf("run out = %v/%v, want none with 10 points of credit", a.RunOutAtRecent, a.HoursEarlyRecent)
	}
	// Pace line, headroom and allowance per 24 hours stay on the 100-point allowance.
	if a.PaceLine != baseline.PaceLine || a.Headroom != baseline.Headroom || a.AllowancePer24h != baseline.AllowancePer24h {
		t.Errorf("pace line/headroom/allowance moved with extra credit")
	}

	in.Policy = PolicyUseAll
	if got := Advise(in).Advice; got.Headline != "Within budget: 3 lanes end the week at 106%, 4 points unused at the reset." || got.LanesSuggested != 3 {
		t.Errorf("use-all with credit: %d %q", got.LanesSuggested, got.Headline)
	}
	in.Policy = PolicyPace
	if got := Advise(in).Advice; got.Headline != "Over budget for the week at 3 lanes (106% at the reset). 2 lanes end at 96.1%." || got.LanesSuggested != 2 {
		t.Errorf("pace with credit: the target should be 105; got %d %q", got.LanesSuggested, got.Headline)
	}
	in.Policy = PolicyFinishEarly
	f := Advise(in).Advice.Finish
	near(t, "spare points use the ceiling", *f.SparePoints, 15.2, 0.05)

	// A smaller top-up still reaches the ceiling, only later.
	in.Policy, in.ExtraCreditPoints = "", 2
	b := Advise(in)
	if b.RunOutAtRecent == nil || !b.RunOutAtRecent.After(*baseline.RunOutAtRecent) {
		t.Fatalf("run out with 2 points of credit = %v, want later than %v", b.RunOutAtRecent, baseline.RunOutAtRecent)
	}
	if b.HoursEarlyRecent >= baseline.HoursEarlyRecent {
		t.Errorf("hours early did not shrink: %v vs %v", b.HoursEarlyRecent, baseline.HoursEarlyRecent)
	}
	if got := b.ByLanes[3]; got.RunOutAt == nil || !got.RunOutAt.Equal(*b.RunOutAtRecent) {
		t.Errorf("lane table run out disagrees with the headline run out")
	}

	// Zero, negative and not-a-number credit all change nothing.
	for _, extra := range []float64{0, -5, math.NaN(), math.Inf(1) * -1} {
		in.ExtraCreditPoints = extra
		if got := Advise(in); !reflect.DeepEqual(got, Advise(withPolicy(workedExample(t), ""))) || got.Ceiling != 100 {
			t.Errorf("extra credit %v changed the result (ceiling %v)", extra, got.Ceiling)
		}
	}
}

func withPolicy(in Input, policy string) Input {
	in.Policy = policy
	return in
}

func TestAdviseZeroBurnFinish(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyFinishEarly
	in.Readings[0] = percentReading(at(t, 2026, 10, 23, 17, 9), 72) // nothing added since the previous reading
	a := Advise(in)
	if a.RecentBurnPer24h != 0 || a.PerLanePer24h != 0 {
		t.Fatalf("recent burn/per lane = %v/%v, want 0/0", a.RecentBurnPer24h, a.PerLanePer24h)
	}
	f := a.Advice.Finish
	if f == nil || !f.NoBurn || f.BeforeAllowance || f.Hours != nil || f.At != nil || f.EndPercent != nil || f.SparePoints != nil {
		t.Errorf("finish = %+v, want only no_burn", f)
	}
	if want := "No lane is burning above the idle baseline, so no finish time can be given."; a.Advice.Headline != want {
		t.Errorf("headline = %q, want %q", a.Advice.Headline, want)
	}
	if a.Advice.LanesSuggested != 3 {
		t.Errorf("lanes suggested = %d, want 3", a.Advice.LanesSuggested)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if s := string(raw); strings.Contains(s, "Inf") || strings.Contains(s, "NaN") {
		t.Errorf("output holds a non-finite value: %s", s)
	}
	if !strings.Contains(string(raw), `"no_burn":true`) {
		t.Errorf("no_burn missing from %s", raw)
	}

	// A recent burn at or below the baseline gives the same result.
	in.Readings[0] = percentReading(at(t, 2026, 10, 23, 17, 9), 71.9)
	if got := Advise(in).Advice.Finish; got == nil || !got.NoBurn {
		t.Errorf("burn below baseline: finish = %+v", got)
	}
	// So does a window with no lanes.
	in = workedExample(t)
	in.Policy, in.RecentLanes = PolicyFinishEarly, intp(0)
	if got := Advise(in).Advice.Finish; got == nil || !got.NoBurn {
		t.Errorf("no recent lanes: finish = %+v", got)
	}
}

func TestAdviseSpendOnlyReading(t *testing.T) {
	spend := Reading{ID: "api-1", At: at(t, 2026, 10, 23, 22, 8), SpendUSD: f64(41.5), SessionPercent: f64(9), Source: "api"}
	in := workedExample(t)
	in.Readings = []Reading{in.Readings[0], spend}
	for _, policy := range Policies() {
		in.Policy = policy
		a := Advise(in)
		if a.Needs != "percent" {
			t.Fatalf("%s: Needs = %q, want percent", policy, a.Needs)
		}
		if !strings.Contains(a.Advice.Headline, "weekly percentage is needed") {
			t.Errorf("%s: headline %q does not ask for a weekly percentage", policy, a.Advice.Headline)
		}
		if a.Verdict != "" || len(a.ByLanes) != 0 || a.Chosen != nil || a.Advice.Finish != nil {
			t.Errorf("%s: a spend reading produced a verdict: %+v", policy, a)
		}
		if a.Advice.Notes[0] != operatorChoosesNote || a.Advice.Policy != policy {
			t.Errorf("%s: notes/policy = %v/%s", policy, a.Advice.Notes, a.Advice.Policy)
		}
		if a.Session == nil || a.Session.Percent != 9 || a.ReadAt == nil || a.LastResetAt == nil {
			t.Errorf("%s: the week and session should still be reported", policy)
		}
		if len(a.Series) != 1 {
			t.Errorf("%s: series = %+v, want the one percentage reading", policy, a.Series)
		}
	}
	// A spend reading older than a percentage reading does not block the verdict.
	in.Readings = []Reading{{ID: "old", At: at(t, 2026, 10, 23, 12, 0), SpendUSD: f64(30)}, workedExample(t).Readings[0], workedExample(t).Readings[1]}
	if a := Advise(in); a.Needs != "" || a.Verdict != VerdictOver {
		t.Errorf("older spend reading: needs %q verdict %q", a.Needs, a.Verdict)
	}
}

func TestAdviseNeeds(t *testing.T) {
	in := workedExample(t)
	in.Reset = ResetSpec{Weekday: time.Sunday, Clock: "8am", Zone: "America/New_York"}
	if a := Advise(in); a.Needs != "reset" || a.ReadAt != nil || !strings.Contains(a.Advice.Headline, "time zone name") || len(a.Advice.Notes) != 1 {
		t.Errorf("invalid reset: %+v", a)
	}
	in = workedExample(t)
	in.Readings = nil
	if a := Advise(in); a.Needs != "reading" || a.Advice.Headline != "Enter a weekly reading to see advice." {
		t.Errorf("no readings: %+v", a)
	}
	in.Readings = []Reading{{ID: "x"}, {ID: "y", At: at(t, 2026, 10, 23, 1, 0)}, {ID: "z", At: at(t, 2026, 10, 23, 1, 0), Percent: f64(-4)},
		{ID: "n", At: at(t, 2026, 10, 23, 1, 0), Percent: f64(math.NaN()), SpendUSD: f64(math.Inf(1))}}
	if a := Advise(in); a.Needs != "reading" {
		t.Errorf("unusable readings: Needs = %q, want reading", a.Needs)
	}
}

func TestAdviseOrdersAndFiltersReadings(t *testing.T) {
	in := workedExample(t)
	want := Advise(in)
	in.Readings = []Reading{in.Readings[1], percentReading(at(t, 2026, 10, 10, 9, 0), 55), in.Readings[0]}
	got := Advise(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reading order or an old-week reading changed the result:\n got %+v\nwant %+v", got.Series, want.Series)
	}
	if len(got.Series) != 2 {
		t.Errorf("series = %+v, want only this week's readings", got.Series)
	}
}

func TestAdviseSettingsDefaultsAndLimits(t *testing.T) {
	in := Input{Readings: workedExample(t).Readings, Reset: sundayEight(), Lanes: 7}
	a := Advise(in)
	if a.MaxLanes != 3 || a.Lanes != 3 || a.RecentLanes != 3 || a.BaselinePer24h != 3 || a.SafetyPoints != 5 {
		t.Errorf("defaults: max %d lanes %d recent %d baseline %v safety %v", a.MaxLanes, a.Lanes, a.RecentLanes, a.BaselinePer24h, a.SafetyPoints)
	}
	if a.Advice.Policy != PolicyUseAll {
		t.Errorf("default policy = %s", a.Advice.Policy)
	}
	in.Lanes, in.MaxLanes = -2, 500
	if a = Advise(in); a.MaxLanes != 100 || a.Lanes != 0 || len(a.ByLanes) != 101 || a.Chosen.Lanes != 0 {
		t.Errorf("limits: max %d lanes %d rows %d", a.MaxLanes, a.Lanes, len(a.ByLanes))
	}
	// Zero is a real value for the safety margin and the baseline.
	in = workedExample(t)
	in.SafetyPoints, in.BaselinePer24h = f64(0), f64(0)
	a = Advise(in)
	if a.SafetyPoints != 0 || a.BaselinePer24h != 0 || a.ByLanes[0].BurnPer24h != 0 {
		t.Errorf("zero settings were replaced: safety %v baseline %v", a.SafetyPoints, a.BaselinePer24h)
	}
	in.Policy = PolicyPace
	if got := Advise(in).Advice.LanesSuggested; got != 2 {
		// with safety 0 the target is 100, so 2 lanes (96.1) fit, and 3 lanes (106) do not
		t.Errorf("pace with a zero margin suggested %d lanes, want 2", got)
	}
	// Non-numbers fall back to the defaults.
	in.SafetyPoints, in.BaselinePer24h = f64(math.NaN()), f64(math.Inf(1))
	if a = Advise(in); a.SafetyPoints != 5 || a.BaselinePer24h != 3 {
		t.Errorf("non-finite settings: safety %v baseline %v", a.SafetyPoints, a.BaselinePer24h)
	}
	// RecentLanes: nil follows Lanes, an explicit value wins.
	in = workedExample(t)
	in.Lanes = 2
	onePlace(t, "per lane with recent lanes = lanes", Advise(in).PerLanePer24h, 10.5)
	in.RecentLanes = intp(3)
	onePlace(t, "per lane with recent lanes 3", Advise(in).PerLanePer24h, 7.0)
	in.RecentLanes = intp(-1)
	if got := Advise(in); got.RecentLanes != 0 || got.PerLanePer24h != 0 {
		t.Errorf("negative recent lanes: %d per lane %v", got.RecentLanes, got.PerLanePer24h)
	}
}

func TestAdviseRemainingWorkEstimateAndTyped(t *testing.T) {
	cases := []struct {
		name       string
		work       RemainingWork
		inUse      string
		points     float64
		estimate   *float64
		typed      *float64
		wantHeader string
	}{
		{"estimate only", RemainingWork{QueueTasks: 10, CostPerTask: 1.5}, "estimate", 15, f64(15), nil, ""},
		{"typed replaces the estimate", RemainingWork{QueueTasks: 10, CostPerTask: 1.5, Typed: f64(20)}, "typed", 20, f64(15), f64(20), ""},
		{"typed without a queue", RemainingWork{Typed: f64(8)}, "typed", 8, nil, f64(8), ""},
		{"typed zero is a real figure", RemainingWork{QueueTasks: 4, CostPerTask: 2, Typed: f64(0)}, "typed", 0, f64(8), f64(0), ""},
		{"empty queue with a cost is an estimate of zero", RemainingWork{CostPerTask: 2}, "estimate", 0, f64(0), nil, ""},
		{"nothing entered", RemainingWork{}, "none", 0, nil, nil, "Enter the remaining work"},
		{"queue without a cost", RemainingWork{QueueTasks: 5}, "none", 0, nil, nil, "Enter the remaining work"},
		{"invalid typed falls back to the estimate", RemainingWork{QueueTasks: 2, CostPerTask: 3, Typed: f64(-1)}, "estimate", 6, f64(6), nil, ""},
		{"not-a-number typed is ignored", RemainingWork{Typed: f64(math.NaN())}, "none", 0, nil, nil, "Enter the remaining work"},
		{"absurd typed is ignored", RemainingWork{Typed: f64(1e12)}, "none", 0, nil, nil, "Enter the remaining work"},
		{"absurd estimate is ignored", RemainingWork{QueueTasks: math.MaxInt32, CostPerTask: 1e6}, "none", 0, nil, nil, "Enter the remaining work"},
		{"negative queue counts as zero", RemainingWork{QueueTasks: -3, CostPerTask: 2}, "estimate", 0, f64(0), nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := workedExample(t)
			in.Policy = PolicyFinishEarly
			in.RemainingWork = c.work
			a := Advise(in)
			w := a.RemainingWork
			if w.InUse != c.inUse || w.Points != c.points {
				t.Fatalf("in use %q points %v, want %q %v", w.InUse, w.Points, c.inUse, c.points)
			}
			if !reflect.DeepEqual(w.EstimatePoints, c.estimate) || !reflect.DeepEqual(w.TypedPoints, c.typed) {
				t.Errorf("estimate %v typed %v, want %v %v", w.EstimatePoints, w.TypedPoints, c.estimate, c.typed)
			}
			if c.wantHeader != "" {
				if !strings.HasPrefix(a.Advice.Headline, c.wantHeader) || a.Advice.Finish != nil {
					t.Errorf("headline %q finish %v", a.Advice.Headline, a.Advice.Finish)
				}
				return
			}
			// The advice uses the figure in use.
			f := a.Advice.Finish
			if f == nil || f.NoBurn {
				t.Fatalf("finish = %+v", f)
			}
			near(t, "finish hours", *f.Hours, c.points/(3*a.PerLanePer24h)*24, 1e-9)
		})
	}
	// The work figures are part of the reading, not of the policy.
	in := workedExample(t)
	in.RemainingWork = RemainingWork{QueueTasks: 3, CostPerTask: 2, Typed: f64(20)}
	if w := Advise(in).RemainingWork; w.QueueTasks != 3 || w.CostPerTask != 2 {
		t.Errorf("queue/cost echo = %d/%v", w.QueueTasks, w.CostPerTask)
	}
}

func TestAdviseOverBudgetFinish(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyFinishEarly
	in.RemainingWork = RemainingWork{Typed: f64(60)}
	a := Advise(in)
	f := a.Advice.Finish
	if f.BeforeAllowance || *f.SparePoints >= 0 {
		t.Fatalf("finish = %+v, want over budget", f)
	}
	want := "Over budget: the allowance runs out before the work finishes (" + formatPoints(*f.SparePoints) + " points short). Consider pacing or running until used up."
	if a.Advice.Headline != want || !strings.Contains(want, "(-") {
		t.Errorf("headline = %q, want %q", a.Advice.Headline, want)
	}
	// Enough spare points but too slow: the shortfall reads 0.
	in.RemainingWork = RemainingWork{Typed: f64(1)}
	in.Readings[1] = percentReading(at(t, 2026, 10, 25, 7, 0), 72) // one hour before the reset
	in.Readings[0] = percentReading(at(t, 2026, 10, 25, 1, 0), 71)
	a = Advise(in)
	if f = a.Advice.Finish; f.BeforeAllowance || *f.SparePoints < 0 {
		t.Fatalf("finish = %+v, want too slow with points to spare", f)
	}
	if !strings.Contains(a.Advice.Headline, "(0 points short)") {
		t.Errorf("headline = %q", a.Advice.Headline)
	}
}

// TestAdviseInvariantsHold sweeps many inputs and checks the rules every result must keep.
func TestAdviseInvariantsHold(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	reset := at(t, 2026, 10, 18, 8, 0)
	for i := 0; i < 400; i++ {
		prevAt := reset.Add(time.Duration(rng.Intn(150*60)) * time.Minute)
		newAt := prevAt.Add(time.Duration(rng.Intn(30*60)) * time.Minute)
		prev := rng.Float64() * 100
		cur := prev + rng.Float64()*20 - 3
		if cur < 0 {
			cur = 0
		}
		in := Input{
			Readings:          []Reading{percentReading(prevAt, prev), percentReading(newAt, cur)},
			Reset:             sundayEight(),
			Now:               newAt.Add(time.Duration(rng.Intn(600)) * time.Minute),
			ExtraCreditPoints: float64(rng.Intn(3)) * 10,
			Lanes:             rng.Intn(6) - 1,
			MaxLanes:          rng.Intn(6),
			BaselinePer24h:    f64(rng.Float64() * 8),
			SafetyPoints:      f64(float64(rng.Intn(12))),
			RemainingWork:     RemainingWork{Typed: f64(rng.Float64() * 60)},
		}
		if rng.Intn(4) == 0 {
			in.RecentLanes = intp(rng.Intn(4))
		}
		var numbers *Advice
		for _, policy := range Policies() {
			in.Policy = policy
			a := Advise(in)
			if _, err := json.Marshal(a); err != nil {
				t.Fatalf("iteration %d %s: %v", i, policy, err)
			}
			checkAdviceRules(t, a, policy)
			cp := a
			cp.Advice = PolicyAdvice{}
			if numbers == nil {
				numbers = &cp
			} else if !reflect.DeepEqual(*numbers, cp) {
				t.Fatalf("iteration %d: policy %s changed a number of the reading", i, policy)
			}
		}
	}
}

func checkAdviceRules(t *testing.T, a Advice, policy string) {
	t.Helper()
	pa := a.Advice
	if pa.Policy != policy {
		t.Errorf("policy = %s, want %s", pa.Policy, policy)
	}
	if len(pa.Notes) == 0 || pa.Notes[0] != "The operator chooses the number of lanes; this is advice only." {
		t.Errorf("%s: first note = %v", policy, pa.Notes)
	}
	if pa.LanesSuggested < 1 {
		t.Errorf("%s: advice names %d lanes", policy, pa.LanesSuggested)
	}
	if policy == PolicyFinishEarly && pa.LanesSuggested != a.MaxLanes {
		t.Errorf("finish-early suggests %d lanes, want all %d", pa.LanesSuggested, a.MaxLanes)
	}
	if pa.LanesSuggested > a.MaxLanes {
		t.Errorf("%s: %d lanes suggested of %d", policy, pa.LanesSuggested, a.MaxLanes)
	}
	if pa.Headline == "" {
		t.Errorf("%s: no headline", policy)
	}
	for _, n := range pa.Notes {
		for _, banned := range []string{"stop all", "stops", "zero lanes"} {
			if strings.Contains(n, banned) {
				t.Errorf("%s: note %q suggests stopping lanes", policy, n)
			}
		}
	}
}

func TestAdviseZeroLaneInvariants(t *testing.T) {
	for _, maxLanes := range []int{1, 2, 3, 4} {
		for _, extra := range []float64{0, 30} {
			for _, end := range []float64{5, 60, 95, 130} {
				for _, policy := range Policies() {
					in := Input{
						Readings: []Reading{
							percentReading(at(t, 2026, 10, 23, 17, 0), end-1),
							percentReading(at(t, 2026, 10, 23, 22, 0), end),
						},
						Reset: sundayEight(), Policy: policy, MaxLanes: maxLanes, ExtraCreditPoints: extra,
						RemainingWork: RemainingWork{Typed: f64(20)},
					}
					a := Advise(in)
					checkAdviceRules(t, a, policy)
					if a.Chosen == nil || a.Chosen.Lanes != a.Lanes {
						t.Errorf("chosen = %+v for lanes %d", a.Chosen, a.Lanes)
					}
				}
			}
		}
	}
}

func TestAdviseJSONContract(t *testing.T) {
	in := workedExample(t)
	in.Readings[1].SessionPercent = f64(15)
	in.Policy = PolicyFinishEarly
	in.RemainingWork = RemainingWork{QueueTasks: 4, CostPerTask: 2, Typed: f64(20)}
	raw, err := json.Marshal(Advise(in))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"days_since_reset", "days_left", "allowance_per_24h", "pace_line", "headroom", "recent_burn_per_24h",
		"week_avg_burn_per_24h", "recent_fallback", "projected_end_recent", "projected_end_avg", "run_out_at_recent",
		"run_out_at_avg", "hours_early_recent", "hours_early_avg", "verdict", "verdict_avg", "ahead_of_pace",
		"per_lane_per_24h", "baseline_per_24h", "by_lanes", "chosen", "advice", "ceiling", "extra_credit_points",
		"remaining_work", "session", "series", "daily", "latest_age_hours",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON is missing %q", key)
		}
	}
	rows := m["by_lanes"].([]any)
	for _, key := range []string{"lanes", "burn_per_24h", "projected_end", "run_out_at", "hours_early"} {
		if _, ok := rows[0].(map[string]any)[key]; !ok {
			t.Errorf("by_lanes row is missing %q", key)
		}
	}
	for key, parent := range map[string]string{
		"policy": "advice", "lanes_suggested": "advice", "headline": "advice", "notes": "advice", "finish": "advice",
		"percent": "session", "read_at": "session",
		"in_use": "remaining_work", "points": "remaining_work", "estimate_points": "remaining_work",
		"typed_points": "remaining_work", "queue_tasks": "remaining_work", "cost_per_task": "remaining_work",
	} {
		if _, ok := m[parent].(map[string]any)[key]; !ok {
			t.Errorf("%s is missing %q", parent, key)
		}
	}
	finish := m["advice"].(map[string]any)["finish"].(map[string]any)
	for _, key := range []string{"hours", "at", "end_percent", "spare_points", "before_allowance", "no_burn"} {
		if _, ok := finish[key]; !ok {
			t.Errorf("finish is missing %q", key)
		}
	}
	if _, ok := m["by_model"]; ok {
		t.Errorf("by_model should be omitted while it is empty")
	}
	a := Advise(in)
	a.ByModel = map[string]float64{"haiku": 4.2}
	raw, _ = json.Marshal(a)
	if !strings.Contains(string(raw), `"by_model":{"haiku":4.2}`) {
		t.Errorf("by_model is not carried: %s", raw)
	}
	var rt Input
	body, _ := json.Marshal(in)
	if err := json.Unmarshal(body, &rt); err != nil || len(rt.Readings) != 2 || rt.Reset.Zone != "America/New_York" {
		t.Errorf("input round trip: %v %+v", err, rt)
	}
}

func TestAdviseWeekAverageAtResetInstant(t *testing.T) {
	// A reading taken at the reset moment has no elapsed time: no division by zero.
	in := Input{Readings: []Reading{percentReading(at(t, 2026, 10, 18, 8, 0), 0)}, Reset: sundayEight(), Lanes: 1}
	a := Advise(in)
	if a.DaysSinceReset != 0 || a.WeekAvgBurnPer24h != 0 || a.PaceLine != 0 {
		t.Errorf("at the reset: %+v", a)
	}
	if _, err := json.Marshal(a); err != nil {
		t.Errorf("marshal: %v", err)
	}
}

func TestFormatPoints(t *testing.T) {
	cases := map[float64]string{106.0: "106", 96.14: "96.1", 5.15: "5.2", 0: "0", -0.04: "0", -3.26: "-3.3", 95: "95", 22.77: "22.8"}
	for in, want := range cases {
		if got := formatPoints(in); got != want {
			t.Errorf("formatPoints(%v) = %q, want %q", in, got, want)
		}
	}
	if lanesWord(1) != "1 lane" || lanesWord(2) != "2 lanes" || lanesEnd(1) != "1 lane ends" || lanesEnd(3) != "3 lanes end" {
		t.Errorf("lane wording is wrong")
	}
}

func TestAdviseLeavesLaneNumbersToTheService(t *testing.T) {
	// Advise is pure and has no ledger: the three lane numbers are the service's to fill,
	// and the running count serialises as null rather than 0.
	a := Advise(workedExample(t))
	if a.LanesRunningNow != nil || a.LanesStamped != nil || a.WhatIfLanes != nil {
		t.Errorf("lane numbers set by Advise: %v %v %v", a.LanesRunningNow, a.LanesStamped, a.WhatIfLanes)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"lanes_running_now", "lanes_stamped"} {
		if v, ok := m[k]; !ok || string(v) != "null" {
			t.Errorf("%s: got %s (present %v); want null", k, v, ok)
		}
	}
	if _, ok := m["what_if_lanes"]; ok {
		t.Errorf("what_if_lanes is present without a what-if")
	}
	n := 2
	a.LanesRunningNow = &n
	raw, _ = json.Marshal(a)
	if !strings.Contains(string(raw), `"lanes_running_now":2`) {
		t.Errorf("lanes_running_now 2 not serialised: %s", raw)
	}
}
