package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// The pace rig resets on Saturday 09:00 UTC and its only reading is from Friday 19:30.
// rolloverNow is one hour into the new week with no new reading.
var rolloverNow = time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

// rolloverBuckets is last week's use and cwMillion million cache-write tokens since the reset.
func rolloverBuckets(cwMillion int64) []burnguard.Bucket {
	return []burnguard.Bucket{
		{Hour: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), Other: burnguard.Tokens{CacheWrite: 1_000_000}},
		{Hour: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC), Other: burnguard.Tokens{CacheWrite: cwMillion * 1_000_000}},
	}
}

// TestPaceHoldsTheNewWeekWhenTheTokenEstimateIsOver: after the reset with no new reading,
// a burn that is over the pace line holds new work.
func TestPaceHoldsTheNewWeekWhenTheTokenEstimateIsOver(t *testing.T) {
	r := newPaceRig(t, "on")
	r.now = rolloverNow
	r.buckets = rolloverBuckets(20) // about 7.5 points against a line of about 0.6 plus 2
	r.tick()
	if !r.adm.holdOn() {
		t.Fatalf("not held: %+v", r.pace(t).Entries[0].Last)
	}
	if got := r.pace(t).Entries[0].Rule; got != burnguard.PaceRuleWeek {
		t.Fatalf("rule = %q", got)
	}
	r.ses.assertNoEnd(t)
}

// TestLadderClimbsOnTheTokenEstimateAfterTheReset: the ladder has a projection in the new
// week and climbs when it is over, with the guard-to-advice gap check on.
func TestLadderClimbsOnTheTokenEstimateAfterTheReset(t *testing.T) {
	r := newPaceRig(t, "off")
	r.cfg.MaxProjectionGapPoints = 10
	r.now = rolloverNow
	r.buckets = rolloverBuckets(20)
	r.tick()
	st := r.g.Status()
	if st.LastProjection == nil || st.LastProjection.Needs != "" {
		t.Fatalf("no projection after the reset: %+v", st.LastProjection)
	}
	if st.Level != 1 {
		t.Fatalf("level = %d, decision %+v; want the ladder one rung up", st.Level, st.LastDecision)
	}
}

// TestPaceLiftsLastWeeksHoldWhenTheNewWeekIsUnder: a weekly hold lifts after the reset
// when the estimate is under the line, and the decision says it was estimated.
func TestPaceLiftsLastWeeksHoldWhenTheNewWeekIsUnder(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.tick()
	if !r.adm.holdOn() {
		t.Fatal("not held")
	}
	r.now = rolloverNow
	r.buckets = rolloverBuckets(0)
	r.tick()
	e := r.pace(t).Entries[0]
	if r.adm.holdOn() || !e.Last.WeekFromTokens || !e.Last.WeekReset || e.Last.WeekPercent != 0 {
		t.Fatalf("%+v", e.Last)
	}
}

// TestPaceWithNoTokenDataDecidesNoWeek: with nothing to estimate from, the week side
// decides nothing and says the week reset, as before.
func TestPaceWithNoTokenDataDecidesNoWeek(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.now = rolloverNow
	r.tick()
	last := r.pace(t).Entries[0].Last
	if r.adm.holdOn() || last.WeekFromTokens || !last.WeekReset {
		t.Fatalf("%+v", last)
	}
	if p := r.g.Status().LastProjection; p == nil || p.Needs == "" {
		t.Fatalf("projection = %+v, want one that needs a reading", p)
	}
}

// TestAdviceAndGuardReadOneEstimate: the daemon's advice estimate is the guard's, with the
// guard off too, and it reuses the tick's read of the token data for two intervals.
func TestAdviceAndGuardReadOneEstimate(t *testing.T) {
	r := newPaceRig(t, "shadow")
	r.now = rolloverNow
	r.buckets = rolloverBuckets(20)
	var since []time.Time
	inner := r.g.in.Tokens
	r.g.in.Tokens = func(ctx context.Context, entry string, first bool, from time.Time) ([]burnguard.Bucket, time.Time, error) {
		since = append(since, from)
		return inner(ctx, entry, first, from)
	}
	r.tick()
	proj := r.g.Status().LastProjection
	readings, _, reset, err := r.g.in.Readings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.cfg.Enabled = false
	est := r.g.WeekEstimate(context.Background(), readings, reset, r.now)
	if proj == nil || est == nil || !proj.Estimated || est.Points != proj.EstimatedNow || est.PointsPerHour != proj.PointsPerHour {
		t.Fatalf("projection %+v, advice estimate %+v", proj, est)
	}
	if len(since) != 1 {
		t.Fatalf("token reads = %d, want only the tick's", len(since))
	}
	later := r.now.Add(2 * r.g.interval(*r.cfg))
	r.g.WeekEstimate(context.Background(), readings, reset, later)
	r.g.WeekEstimate(context.Background(), readings, reset, later.Add(time.Minute))
	if len(since) != 2 {
		t.Fatalf("token reads = %d, want one more once the tick's data is old", len(since))
	}
}

// TestTokensSinceReachesOnlyAsFarAsTheCalibration: the running week's 8 days, further only
// in a week with no reading whose calibration readings are older, and never past 15 days.
func TestTokensSinceReachesOnlyAsFarAsTheCalibration(t *testing.T) {
	reset := usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC) }
	var readings []usagecalc.Reading
	for day := 20; day <= 30; day++ { // one reading a day, the last on Friday the 30th
		readings = append(readings, usagecalc.Reading{At: at(day, 12), Percent: fptrUsage(float64(day))})
	}
	for _, c := range []struct {
		name      string
		now, want time.Time
	}{
		{"a reading in the week", at(30, 20), at(22, 20)},
		{"just after the reset", at(31, 10), at(23, 10)},    // the 7th newest reading is within 8 days
		{"late in the unread week", at(36, 10), at(24, 11)}, // an hour before it
		{"never past 15 days", at(45, 10), at(30, 10)},
	} {
		if got := tokensSince(readings, reset, c.now); !got.Equal(c.want) {
			t.Errorf("%s: tokens since %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAttachedCalculatorUsesTheGuardEstimate: the daemon's wiring gives the usage advice
// the attached burn guard's estimate.
func TestAttachedCalculatorUsesTheGuardEstimate(t *testing.T) {
	s := newUsageCalcServer(t)
	// A reading from before the last weekly reset, whatever day the test runs on.
	at := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if rr := doUsage(t, s, http.MethodPost, "/api/usage/readings",
		jsonMarshalUsage(t, usagecalc.Reading{At: at, Percent: fptrUsage(91), Source: usagecalc.SourceTyped}), s.handleUsageReadings); rr.Code != http.StatusOK {
		t.Fatalf("POST reading = %d: %s", rr.Code, rr.Body.String())
	}
	g, err := NewBurnGuard(BurnGuardInputs{
		Config: config.DefaultBurnGuard,
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			return nil, 0, usagecalc.ResetSpec{}, nil
		},
		Tokens: func(context.Context, string, bool, time.Time) ([]burnguard.Bucket, time.Time, error) {
			return []burnguard.Bucket{{Hour: time.Now().UTC().Add(-14 * 24 * time.Hour).Truncate(time.Hour),
				Other: burnguard.Tokens{CacheWrite: 1_000_000}}}, time.Time{}, nil
		},
	}, newFakeAdmission(1), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.burnGuard = g
	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice)
	var res usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("GET advice = %d: %s: %v", rr.Code, rr.Body.String(), err)
	}
	if !res.Stale || res.StaleReason != usagecalc.ResetPassedReason || !res.Advice.Estimated || res.Advice.Percent == 91 || rr.Header().Get("X-Usage-Stale") == "" {
		t.Fatalf("advice = %s", rr.Body.String())
	}
}
