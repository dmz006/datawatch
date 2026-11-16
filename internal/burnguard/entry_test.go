package burnguard

import (
	"math"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

var entryT0 = time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC) // Friday; reset Saturday 09:00

func entryReset() usagecalc.ResetSpec {
	return usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}
}

func TestCoefficientsFromRateUsesTheModelRate(t *testing.T) {
	c := CoefficientsFromRate(ModelRate{InPerK: 0.003, OutPerK: 0.015, CacheReadPerK: 0.0003, CacheWritePerK: 0.00375}, 1)
	if c.InputPerM != 3 || c.OutputPerM != 15 || math.Abs(c.CacheReadPerM-0.3) > 1e-12 || math.Abs(c.CacheWritePerM-3.75) > 1e-12 {
		t.Fatalf("coefficients = %+v", c)
	}
	if p := CoefficientsFromRate(ModelRate{OutPerK: 0.015}, 2); p.OutputPerM != 30 {
		t.Fatalf("points per dollar not applied: %+v", p)
	}
}

func TestProjectMeteredFromSpend(t *testing.T) {
	v := func(f float64) *float64 { return &f }
	in := ProjectInput{
		Readings: []usagecalc.Reading{
			{At: entryT0.Add(-50 * time.Hour), SpendUSD: v(100)},
			{At: entryT0.Add(-2 * time.Hour), SpendUSD: v(148)},
			{At: entryT0.Add(-time.Hour), Percent: v(99)}, // a percent reading is not spend
		},
		Reset: entryReset(), Now: entryT0,
	}
	p := ProjectMetered(in, 200)
	if p.Needs != "" {
		t.Fatalf("needs %q", p.Needs)
	}
	// $48 over 48 hours = $1/h; 13 hours left.
	if p.PercentAtReading != 148 || math.Abs(p.PointsPerHour-1) > 1e-9 || math.Abs(p.Projected-161) > 1e-9 {
		t.Fatalf("projection = %+v", p)
	}
	if p.Ceiling != 200 || p.Target != 190 || p.Over {
		t.Fatalf("target = %v/%v over=%v", p.Ceiling, p.Target, p.Over)
	}
	if q := ProjectMetered(in, 0); q.Needs != NeedsTarget {
		t.Fatalf("no target: needs %q", q.Needs)
	}
	if q := ProjectMetered(ProjectInput{Reset: entryReset(), Now: entryT0}, 200); q.Needs != usagecalc.NeedsReading {
		t.Fatalf("no spend readings: needs %q", q.Needs)
	}
}

func TestProjectEntryPricesTokensPerModel(t *testing.T) {
	v := func(f float64) *float64 { return &f }
	buckets := []Bucket{{Hour: entryT0.Add(-time.Hour), Worker: Tokens{Output: 2_000_000}}}
	base := ProjectInput{
		Readings: []usagecalc.Reading{{At: entryT0.Add(-50 * time.Hour), SpendUSD: v(10)}, {At: entryT0.Add(-2 * time.Hour), SpendUSD: v(20)}},
		Reset:    entryReset(), Now: entryT0, Buckets: buckets, Through: entryT0,
	}
	a, b := base, base
	a.Coef = CoefficientsFromRate(ModelRate{OutPerK: 0.01}, 1)
	b.Coef = CoefficientsFromRate(ModelRate{OutPerK: 0.05}, 1)
	pa := ProjectEntry(EntryProjectInput{ProjectInput: a, Metered: true, PeriodTarget: 500})
	pb := ProjectEntry(EntryProjectInput{ProjectInput: b, Metered: true, PeriodTarget: 500})
	if math.Abs(pa.PointsSinceReading-20) > 1e-9 || math.Abs(pb.PointsSinceReading-100) > 1e-9 {
		t.Fatalf("since reading = %v / %v, want 20 / 100", pa.PointsSinceReading, pb.PointsSinceReading)
	}
	if !(pb.Projected > pa.Projected) {
		t.Fatalf("dearer model projected %v, cheaper %v", pb.Projected, pa.Projected)
	}
}

func TestLadderActsOnItsEntryOnly(t *testing.T) {
	l, _ := NewEntryLadder(nil, EntryTarget{Backend: "metered-x", RouteTo: "", HeldModels: []string{"m1"}})
	s := settings()
	s.Ladder = []string{RungRoutePiPrompt, RungHoldClaude} // the legacy names: accepted, reported generic
	d, _ := l.Tick(TickInput{Projection: proj(20), Settings: s, Now: entryT0, InflightCap: 2})
	if d.Rung != RungRouteEntry || len(d.Actions) != 0 {
		t.Fatalf("route rung with no route target should change nothing: %+v", d)
	}
	d, _ = l.Tick(TickInput{Projection: proj(20), Settings: s, Now: entryT0, InflightCap: 2})
	if len(d.Actions) != 1 || d.Actions[0].Backend != "metered-x" || len(d.Actions[0].Models) != 1 || d.Actions[0].Models[0] != "m1" {
		t.Fatalf("hold actions = %+v", d.Actions)
	}
}
