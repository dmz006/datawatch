package usagecalc

import (
	"math"
	"testing"
	"time"
)

// designSeries is the real operator-reported series since the reset of Sunday 2026-10-18
// 08:00, from the design document, ending with the 22:08 reading of 2026-10-23.
func designSeries(t testing.TB) []Reading {
	rows := []struct {
		d, h, m int
		pct     float64
	}{
		{18, 10, 20, 2}, {18, 14, 30, 4}, {18, 17, 0, 5}, {18, 21, 5, 7}, {19, 9, 55, 9}, {19, 17, 50, 13},
		{19, 20, 15, 14}, {20, 4, 31, 16}, {20, 16, 40, 19}, {21, 14, 40, 30}, {21, 18, 30, 33}, {21, 19, 40, 34},
		{22, 12, 36, 45}, {22, 20, 44, 55}, {23, 2, 40, 58}, {23, 17, 9, 67}, {23, 22, 8, 72},
	}
	out := make([]Reading, 0, len(rows))
	for _, r := range rows {
		out = append(out, percentReading(at(t, 2026, 10, r.d, r.h, r.m), r.pct))
	}
	return out
}

func sumPoints(days []DayUse) float64 {
	total := 0.0
	for _, d := range days {
		total += d.Points
	}
	return total
}

func TestDailyUseMatchesDesignTable(t *testing.T) {
	a := Advise(Input{Readings: designSeries(t), Reset: sundayEight(), Lanes: 3})
	want := []float64{8.7, 8.2, 9.8, 15.3, 19.3, 10.7}
	if len(a.Daily) != len(want) {
		t.Fatalf("daily has %d days, want %d: %+v", len(a.Daily), len(want), a.Daily)
	}
	for i, d := range a.Daily {
		onePlace(t, "day points", d.Points, want[i])
		if !d.Estimated {
			t.Errorf("day %d is not flagged estimated", i)
		}
		if d.Partial != (i == len(want)-1) {
			t.Errorf("day %d partial = %v", i, d.Partial)
		}
		if i > 0 && !d.Start.Equal(a.Daily[i-1].End) {
			t.Errorf("day %d does not start where day %d ends", i, i-1)
		}
	}
	near(t, "daily total", sumPoints(a.Daily), 72, 1e-9)
	if !a.Daily[0].Start.Equal(*a.LastResetAt) || !a.Daily[5].End.Equal(*a.ReadAt) {
		t.Errorf("daily does not span the reset to the newest reading")
	}
}

func TestDailyUseBoundariesFollowDaylightSaving(t *testing.T) {
	loc := newYork(t)
	// The week of 2026-10-25 to 2026-11-01 holds the clock change on Sunday morning.
	in := Input{
		Readings: []Reading{
			percentReading(at(t, 2026, 10, 25, 12, 0), 10),
			percentReading(at(t, 2026, 10, 28, 8, 0), 40), // exactly on a day boundary
			percentReading(at(t, 2026, 11, 1, 7, 0), 70),
		},
		Reset: sundayEight(), Lanes: 2,
	}
	a := Advise(in)
	if len(a.Daily) != 7 {
		t.Fatalf("daily has %d days, want 7", len(a.Daily))
	}
	for i, d := range a.Daily {
		if l := d.Start.In(loc); l.Hour() != 8 || l.Minute() != 0 {
			t.Errorf("day %d starts at %v, want 08:00 local", i, l)
		}
	}
	last := a.Daily[6]
	if got := last.End.Sub(last.Start); got != 24*time.Hour {
		t.Errorf("last day runs %v, want 24h (08:00 EDT to 07:00 EST)", got)
	}
	if !last.Partial || !last.End.Equal(in.Readings[2].At) {
		t.Errorf("last day = %+v, want partial up to the newest reading", last)
	}
	// The day that holds the clock change is 25 hours long in real time.
	if got := a.Daily[6].Start.Add(25 * time.Hour); got.In(loc).Hour() != 8 {
		t.Errorf("the day of the change is not 25h: %v", got.In(loc))
	}
	if got := a.Daily[5].End.Sub(a.Daily[5].Start); got != 24*time.Hour {
		t.Errorf("an ordinary day runs %v", got)
	}
	near(t, "daily total", sumPoints(a.Daily), 70, 1e-9)
	// Day 3 ends exactly on a reading and day 3's start lies between readings.
	if !a.Daily[2].Estimated {
		t.Errorf("a day starting between readings must be estimated")
	}
}

func TestDailyUseExactDaysAreNotEstimated(t *testing.T) {
	in := Input{
		Readings: []Reading{
			percentReading(at(t, 2026, 10, 19, 8, 0), 12),
			percentReading(at(t, 2026, 10, 20, 8, 0), 30),
			percentReading(at(t, 2026, 10, 20, 20, 0), 36),
		},
		Reset: sundayEight(), Lanes: 1,
	}
	a := Advise(in)
	if len(a.Daily) != 3 {
		t.Fatalf("daily = %+v", a.Daily)
	}
	wantPoints := []float64{12, 18, 6}
	for i, d := range a.Daily {
		near(t, "points", d.Points, wantPoints[i], 1e-9)
	}
	if a.Daily[0].Estimated || a.Daily[1].Estimated {
		t.Errorf("days that start and end on readings were flagged estimated: %+v", a.Daily)
	}
	if a.Daily[2].Estimated {
		t.Errorf("a partial day that starts on a reading and ends on the newest one is exact: %+v", a.Daily[2])
	}
	if !a.Daily[2].Partial {
		t.Errorf("the last day should be partial")
	}
}

func TestDailyUseNewestOnADayBoundary(t *testing.T) {
	in := Input{
		Readings: []Reading{percentReading(at(t, 2026, 10, 19, 20, 0), 20), percentReading(at(t, 2026, 10, 20, 8, 0), 26)},
		Reset:    sundayEight(), Lanes: 1,
	}
	a := Advise(in)
	if len(a.Daily) != 2 || a.Daily[1].Partial || !a.Daily[1].End.Equal(in.Readings[1].At) {
		t.Fatalf("daily = %+v, want two whole days and no empty third", a.Daily)
	}
	near(t, "daily total", sumPoints(a.Daily), 26, 1e-9)
}

func TestDailyUseSingleDayAndEmpty(t *testing.T) {
	a := Advise(Input{Readings: []Reading{percentReading(at(t, 2026, 10, 18, 20, 0), 6)}, Reset: sundayEight(), Lanes: 1})
	if len(a.Daily) != 1 || !a.Daily[0].Partial || math.Abs(a.Daily[0].Points-6) > 1e-9 {
		t.Errorf("first day only: %+v", a.Daily)
	}
	// A reading at the reset moment itself leaves no day to report.
	b := Advise(Input{Readings: []Reading{percentReading(at(t, 2026, 10, 18, 8, 0), 0)}, Reset: sundayEight(), Lanes: 1})
	if b.Daily == nil || len(b.Daily) != 0 {
		t.Errorf("at the reset: %+v", b.Daily)
	}
	// Needs-states keep the list empty, not null.
	if c := Advise(Input{Reset: sundayEight()}); c.Daily == nil || c.Series == nil || c.ByLanes == nil {
		t.Errorf("lists should be empty, not nil, when the operator must act")
	}
}

func TestDailyUseReadingAtResetInstantIsIgnoredForShape(t *testing.T) {
	// A reading on the reset instant counts as 0 for the interpolation; later readings carry the use.
	in := Input{
		Readings: []Reading{percentReading(at(t, 2026, 10, 18, 8, 0), 0), percentReading(at(t, 2026, 10, 18, 20, 0), 6)},
		Reset:    sundayEight(), Lanes: 1,
	}
	a := Advise(in)
	if len(a.Series) != 2 || len(a.Daily) != 1 {
		t.Fatalf("series %d daily %d", len(a.Series), len(a.Daily))
	}
	near(t, "points", a.Daily[0].Points, 6, 1e-9)
	if a.Daily[0].Estimated {
		t.Errorf("start is the reset and end is a reading, so the day is exact")
	}
}

func TestPercentAtInterpolation(t *testing.T) {
	base := at(t, 2026, 10, 18, 8, 0)
	pts := []usagePoint{{base, 0}, {base.Add(10 * time.Hour), 10}, {base.Add(10 * time.Hour), 12}, {base.Add(20 * time.Hour), 22}}
	cases := []struct {
		after time.Duration
		want  float64
	}{{-time.Hour, 0}, {0, 0}, {5 * time.Hour, 5}, {10 * time.Hour, 10}, {15 * time.Hour, 17}, {20 * time.Hour, 22}, {30 * time.Hour, 22}}
	for _, c := range cases {
		if got := percentAt(pts, base.Add(c.after)); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("percentAt(+%v) = %v, want %v", c.after, got, c.want)
		}
	}
	// Two points at the same moment never divide by zero.
	dup := []usagePoint{{base, 0}, {base, 3}}
	if got := percentAt(dup, base.Add(-time.Second)); got != 0 {
		t.Errorf("before = %v", got)
	}
	if got := percentAt([]usagePoint{{base, 1}, {base.Add(time.Hour), 5}, {base.Add(time.Hour), 9}}, base.Add(time.Hour)); got != 5 {
		t.Errorf("at a duplicate moment = %v, want 5", got)
	}
	if got := percentAt([]usagePoint{{base.Add(time.Hour), 4}, {base.Add(time.Hour), 6}, {base.Add(2 * time.Hour), 8}}, base.Add(time.Hour)); got != 4 {
		t.Errorf("tie = %v", got)
	}
}
