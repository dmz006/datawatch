package burnguard

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// These tests show the ladder follows the operator's readings when the token estimate runs
// far above them. All data is built here: fake readings, fake hourly buckets and write-only
// coefficients (one point per million cache-write tokens). No daemon, no network.

// defaultGap is the shipped usage.burn_guard.max_projection_gap_points (25).
const defaultGap = 25.0

// frReset is Monday 00:00 UTC; the week of frT0 runs 2026-11-09 to 2026-11-16.
var frReset = usagecalc.ResetSpec{Weekday: time.Monday, Clock: "00:00", Zone: "UTC"}

// frT0 is Wednesday 2026-11-11 00:00 UTC, two days into the week (pace line near 28.6%).
var frT0 = time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)

func frSettings(gap float64) LadderSettings {
	return LadderSettings{Enabled: true, StepBack: true, MarginPoints: 2, MaxProjectionGapPoints: gap}
}

// frProjection builds a projection by hand with the target at 98.
func frProjection(guard, advice, atReading, line float64) Projection {
	return Projection{
		Projected: guard, AdviceProjected: advice, Target: 98, Ceiling: 100, Margin: 2,
		OverBy: guard - 98, Over: guard > 98,
		PercentAtReading: atReading, Line: line,
		PointsPerHour: 1.5, AllowedPerHour: 0.6,
	}
}

func frTick(t *testing.T, l *Ladder, p Projection, s LadderSettings) Decision {
	t.Helper()
	d, err := l.Tick(TickInput{Projection: p, Settings: s, Now: frT0, InflightCap: 3})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newFRLadder(t *testing.T) *Ladder {
	t.Helper()
	l, err := NewLadder(&memStore{})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// frSeries is readings every six hours that rise by step points each, from start, ending at
// frT0, with buckets whose cache-write tokens price at skew times each interval's rise.
// The first reading is 6*(n-1) hours before frT0.
func frSeries(n int, start, step, skew float64) ([]usagecalc.Reading, []Bucket) {
	var rs []usagecalc.Reading
	var bs []Bucket
	first := frT0.Add(-time.Duration(6*(n-1)) * time.Hour)
	for i := 0; i < n; i++ {
		at := first.Add(time.Duration(6*i) * time.Hour)
		rs = append(rs, calPct(at, start+step*float64(i)))
		if i < n-1 {
			// Six hourly buckets spread the interval's priced tokens evenly.
			per := int64(step * skew / 6 * 1e6)
			for h := 0; h < 6; h++ {
				bs = append(bs, calBucket(at.Add(time.Duration(h)*time.Hour), per))
			}
		}
	}
	return rs, bs
}

var frTail = 250000.0 * 1.3

func frProject(rs []usagecalc.Reading, bs []Bucket, now time.Time) Projection {
	margin := 2.0
	return Project(ProjectInput{Readings: rs, Reset: frReset, Now: now, Buckets: bs,
		Coef: calWriteOnly, MarginPoints: &margin})
}

// (1) An estimate far above the readings, with the readings projecting under budget, no
// longer reaches hold_claude.
func TestFollowsReadingsHighEstimateDoesNotReachHoldClaude(t *testing.T) {
	// Real projection path: tokens priced 30% above the reading deltas, readings gentle.
	rs, bs := frSeries(6, 20, 1.5, 1.3)
	// Tokens in the hour after the newest reading, also 30% over the readings' pace.
	bs = append(bs, calBucket(frT0, int64(frTail)))
	p := frProject(rs, bs, frT0.Add(time.Hour))
	if p.Needs != "" {
		t.Fatalf("needs %q", p.Needs)
	}
	if p.AdviceProjected >= p.Target {
		t.Fatalf("setup: readings project %.1f, want under target %.1f", p.AdviceProjected, p.Target)
	}
	l := newFRLadder(t)
	for i := 0; i < 4; i++ {
		d := frTick(t, l, p, frSettings(defaultGap))
		if d.Direction != DirectionHold || d.Changed {
			t.Fatalf("tick %d moved the ladder: %+v", i, d)
		}
	}
	if got := l.State().Applied; len(got) != 0 {
		t.Fatalf("applied %v, want none", got)
	}

	// Direct form: the guard number is far over, the readings are under budget and the
	// newest reading is under the pace line. The gap check hands the decision to the lower.
	hi := frProjection(160, 70, 20, 28.6)
	l = newFRLadder(t)
	for i := 0; i < 4; i++ {
		d := frTick(t, l, hi, frSettings(defaultGap))
		if d.Direction != DirectionHold || d.Rung == RungHoldClaude {
			t.Fatalf("tick %d: %+v", i, d)
		}
		if !strings.Contains(d.Reason, "using the lower") {
			t.Fatalf("reason %q should say the lower was used", d.Reason)
		}
	}
	if len(l.State().Applied) != 0 {
		t.Fatalf("applied %v", l.State().Applied)
	}
}

// (2) A reading over the pace line still steps up a rung even when the advice projection is
// lower.
func TestFollowsReadingsOverPaceLineStillClimbs(t *testing.T) {
	p := frProjection(160, 70, 45, 28.6) // reading 45% against a 28.6% line
	l := newFRLadder(t)
	for i, want := range DefaultLadder() {
		d := frTick(t, l, p, frSettings(defaultGap))
		if d.Direction != DirectionClimb || d.Rung != want {
			t.Fatalf("tick %d: %+v, want climb to %s", i, d, want)
		}
	}
	// A guard projection that is not itself over never forces a climb.
	l = newFRLadder(t)
	d := frTick(t, l, frProjection(90, 40, 45, 28.6), frSettings(defaultGap))
	if d.Direction != DirectionHold {
		t.Fatalf("under-target guard moved the ladder: %+v", d)
	}
}

// (3) A step back follows a reading under the line, with StepBack on.
func TestFollowsReadingsStepBackUnderLine(t *testing.T) {
	l := newFRLadder(t)
	if d := frTick(t, l, frProjection(160, 70, 45, 28.6), frSettings(defaultGap)); d.Direction != DirectionClimb {
		t.Fatalf("setup climb: %+v", d)
	}
	// The readings now sit under the line and the advice projects well under: step back,
	// although the token-priced guard number is still over.
	p := frProjection(130, 60, 20, 28.6)
	d := frTick(t, l, p, frSettings(defaultGap))
	if d.Direction != DirectionStepBack || d.Rung != RungLowerInflight || len(l.State().Applied) != 0 {
		t.Fatalf("step back: %+v", d)
	}
	// With StepBack off the same tick holds and the rung stays in force.
	l = newFRLadder(t)
	frTick(t, l, frProjection(160, 70, 45, 28.6), frSettings(defaultGap))
	s := frSettings(defaultGap)
	s.StepBack = false
	d = frTick(t, l, p, s)
	if d.Direction != DirectionHold || len(l.State().Applied) != 1 {
		t.Fatalf("step back off: %+v", d)
	}
}

// (4) The gap setting at its default of 25 and at 0.
func TestFollowsReadingsGapSetting(t *testing.T) {
	// Gap 20 is inside the default 25: the guard number is used untouched and climbs.
	d := frTick(t, newFRLadder(t), frProjection(120, 100, 20, 28.6), frSettings(defaultGap))
	if d.Direction != DirectionClimb || d.Gap != 20 || !approx(d.OverBy, 22) ||
		!strings.Contains(d.AlertText, "using the guard") {
		t.Fatalf("gap inside the limit: %+v", d)
	}
	// Gap 40 is over the default: the lower (advice 80) decides, under target, so hold.
	d = frTick(t, newFRLadder(t), frProjection(120, 80, 20, 28.6), frSettings(defaultGap))
	if d.Direction != DirectionHold || d.Gap != 40 || !approx(d.OverBy, -18) {
		t.Fatalf("gap over the limit: %+v", d)
	}
	// Exactly at the limit is not over it.
	d = frTick(t, newFRLadder(t), frProjection(120, 95, 20, 28.6), frSettings(defaultGap))
	if d.Direction != DirectionClimb || !approx(d.OverBy, 22) {
		t.Fatalf("gap at the limit: %+v", d)
	}
	// Gap setting 0 never compares: the same 40-point gap leaves the guard number alone.
	d = frTick(t, newFRLadder(t), frProjection(120, 80, 20, 28.6), frSettings(0))
	if d.Direction != DirectionClimb || !approx(d.OverBy, 22) || !strings.Contains(d.AlertText, "using the guard") {
		t.Fatalf("gap 0: %+v", d)
	}
	// And a huge gap at 0 still uses the guard untouched.
	d = frTick(t, newFRLadder(t), frProjection(300, 10, 20, 28.6), frSettings(0))
	if d.Direction != DirectionClimb || !approx(d.OverBy, 202) {
		t.Fatalf("gap 0, huge gap: %+v", d)
	}
}

// (5) The decision's AlertText names both projections.
func TestFollowsReadingsAlertTextNamesBothProjections(t *testing.T) {
	// A climb on the guard number.
	d := frTick(t, newFRLadder(t), frProjection(120, 100, 20, 28.6), frSettings(defaultGap))
	if d.Direction != DirectionClimb {
		t.Fatalf("%+v", d)
	}
	for _, want := range []string{"guard 120.0%", "advice 100.0%", "using the guard"} {
		if !strings.Contains(d.AlertText, want) {
			t.Fatalf("alert %q missing %q", d.AlertText, want)
		}
	}
	if d.GuardProjected != 120 || d.AdviceProjected != 100 {
		t.Fatalf("decision numbers: %+v", d)
	}
	// A climb forced by the reading, where the lower was used.
	d = frTick(t, newFRLadder(t), frProjection(160, 70, 45, 28.6), frSettings(defaultGap))
	for _, want := range []string{"guard 160.0%", "advice 70.0%", "using the lower"} {
		if !strings.Contains(d.AlertText, want) {
			t.Fatalf("forced alert %q missing %q", d.AlertText, want)
		}
	}
	// A step back names both too, and so does the question.
	l := newFRLadder(t)
	frTick(t, l, frProjection(160, 70, 45, 28.6), frSettings(defaultGap))
	s := frSettings(defaultGap)
	s.AskOnChange = true
	d = frTick(t, l, frProjection(130, 60, 20, 28.6), s)
	if d.Direction != DirectionStepBack {
		t.Fatalf("%+v", d)
	}
	for _, text := range []string{d.AlertText, d.QuestionText} {
		if !strings.Contains(text, "guard 130.0%") || !strings.Contains(text, "advice 60.0%") {
			t.Fatalf("step back text %q", text)
		}
	}
}

// (6) The calibration ratio stays near the readings-to-estimate ratio over several
// intervals, so the estimate cannot stay a constant factor above the readings.
func TestFollowsReadingsCalibrationTracksReadings(t *testing.T) {
	const skew = 1.3
	rawM := 2.0 / 6 * skew
	rs, bs := frSeries(8, 10, 2, skew) // seven intervals; the newest six are measured
	c := ComputeCalibratedBurn(bs, rs, frT0, frT0, frT0, calWriteOnly)
	want := 1 / skew
	if c.Ratio < want-0.01 || c.Ratio > want+0.01 {
		t.Fatalf("ratio %.4f, want about %.4f", c.Ratio, want)
	}
	// The calibrated estimate of one interval lands on the readings, not 30% over them.
	est := skew * 2 * c.Ratio
	if est < 2*0.99 || est > 2*1.01 {
		t.Fatalf("calibrated estimate %.3f of a 2-point interval", est)
	}

	// The skew drifts from interval to interval; the ratio still follows the readings in
	// total over the newest six intervals.
	var rs2 []usagecalc.Reading
	var bs2 []Bucket
	first := frT0.Add(-42 * time.Hour)
	var points, estimate float64
	for i := 0; i < 8; i++ {
		at := first.Add(time.Duration(6*i) * time.Hour)
		rs2 = append(rs2, calPct(at, 10+2*float64(i)))
		if i < 7 {
			f := 1.1 + 0.05*float64(i)
			bs2 = append(bs2, calBucket(at, int64(2*f*1e6)))
			if i >= 1 { // the newest six intervals
				points += 2
				estimate += 2 * f
			}
		}
	}
	c = ComputeCalibratedBurn(bs2, rs2, frT0, frT0, frT0, calWriteOnly)
	if want := points / estimate; c.Ratio < want-0.01 || c.Ratio > want+0.01 {
		t.Fatalf("drifting skew: ratio %.4f, want about %.4f", c.Ratio, want)
	}

	// Through Project: the points since the newest reading follow the readings' pace, not
	// the 1.3x estimate. One hour of tokens after the newest reading prices at 2/6*1.3.
	bs = append(bs, calBucket(frT0, int64(rawM*1e6)))
	p := frProject(rs, bs, frT0.Add(time.Hour))
	if p.CalibrationRatio < want-0.2 || p.CalibrationRatio > 1 {
		t.Fatalf("projection ratio %.3f", p.CalibrationRatio)
	}
	raw := rawM
	if p.PointsSinceReading >= raw*0.95 {
		t.Fatalf("points since reading %.4f not scaled below the raw estimate %.4f", p.PointsSinceReading, raw)
	}
}
