// burn_guard_week_estimate.go — the estimate of a week that has no usage reading yet. Once a
// weekly reset has passed since the newest reading, the burn guard projects the new week from
// the tokens (burnguard.EstimateWeek) and the usage advice shows the same estimate, so both
// read one set of tokens, prices and calibration.
package server

import (
	"context"
	"time"

	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

const (
	// tokenLookback covers the running week.
	tokenLookback = 8 * 24 * time.Hour
	// tokenLookbackEstimate is the furthest back the estimate of a week with no reading of
	// its own reads, which covers the whole week before it.
	tokenLookbackEstimate = 15 * 24 * time.Hour
)

// tokensSince is the start of the token data an evaluation at now needs: the running week,
// and in a week with no reading of its own as far back as the readings that calibrate it, so
// the ratio does not change as they age. Transcripts are large, so it reads no further.
func tokensSince(readings []usagecalc.Reading, reset usagecalc.ResetSpec, now time.Time) time.Time {
	since := now.Add(-tokenLookback)
	if !usagecalc.ResetPassed(readings, reset, now) {
		return since
	}
	oldest := burnguard.CalibrationSince(readings, now)
	if oldest.IsZero() || !oldest.Add(-time.Hour).Before(since) {
		return since
	}
	if floor := now.Add(-tokenLookbackEstimate); oldest.Add(-time.Hour).Before(floor) {
		return floor
	}
	return oldest.Add(-time.Hour)
}

// estimateTokens is the first entry's token data as last read, by a tick or by the advice.
type estimateTokens struct {
	entry   string
	at      time.Time
	buckets []burnguard.Bucket
	through time.Time
}

// WeekEstimate is the first entry's token estimate of the week that holds now, for the usage
// advice. readings are that entry's readings and reset the advice's weekly reset. It works
// whether or not the guard is enabled and changes no admission. It returns nil when there is
// nothing to estimate from (a metered entry, no token data or no prices).
func (g *BurnGuard) WeekEstimate(ctx context.Context, readings []usagecalc.Reading, reset usagecalc.ResetSpec, now time.Time) *usagecalc.WeekEstimate {
	if g == nil {
		return nil
	}
	entries := g.in.Config().EffectiveEntries()
	if len(entries) == 0 || entries[0].IsMetered() {
		return nil
	}
	e := entries[0]
	coef, priced := g.rateCoefficients(e)
	if !priced {
		coef = g.fitter.Coefficients()
	}

	data := g.estimateData(ctx, e, readings, reset, now)
	est, _, ok := burnguard.EstimateWeek(burnguard.ProjectInput{
		Readings: readings, Reset: reset, Now: now,
		Buckets: data.buckets, Through: data.through, Coef: coef,
	})
	if !ok {
		return nil
	}
	return &est
}

// estimateData is the token data the advice estimate is priced from: what the last tick
// read while it is no older than two intervals, else a read of its own that is reused as
// long. The estimate lock also keeps concurrent advice calls to one read.
func (g *BurnGuard) estimateData(ctx context.Context, e config.BurnGuardEntryConfig, readings []usagecalc.Reading, reset usagecalc.ResetSpec, now time.Time) estimateTokens {
	g.estMu.Lock()
	defer g.estMu.Unlock()
	age := now.Sub(g.est.at)
	if g.est.entry != e.LLM || len(g.est.buckets) == 0 || age < 0 || age >= 2*g.interval(g.in.Config()) {
		buckets, through := g.tokens(ctx, e, true, tokensSince(readings, reset, now))
		g.est = estimateTokens{entry: e.LLM, at: now, buckets: buckets, through: through}
	}
	return g.est
}

// keepEstimateData hands the tick's read of the first entry's token data to the advice.
func (g *BurnGuard) keepEstimateData(entry string, now time.Time, buckets []burnguard.Bucket, through time.Time) {
	g.estMu.Lock()
	defer g.estMu.Unlock()
	g.est = estimateTokens{entry: entry, at: now, buckets: buckets, through: through}
}

// rateCoefficients prices a flat entry's tokens at its model's own cost rate, when it has
// one and a points-per-dollar figure; false means the fitted coefficients apply.
func (g *BurnGuard) rateCoefficients(e config.BurnGuardEntryConfig) (burnguard.Coefficients, bool) {
	if g.in.Rate == nil || e.PointsPerUSD <= 0 {
		return burnguard.Coefficients{}, false
	}
	rate, ok := g.in.Rate(e)
	if !ok || rate.IsZero() {
		return burnguard.Coefficients{}, false
	}
	return burnguard.CoefficientsFromRate(rate, e.PointsPerUSD), true
}

// usageWeekEstimate is the usage calculator's estimator: the burn guard's, once attached.
func (s *Server) usageWeekEstimate(ctx context.Context, readings []usagecalc.Reading, reset usagecalc.ResetSpec, now time.Time) *usagecalc.WeekEstimate {
	return s.burnGuard.WeekEstimate(ctx, readings, reset, now)
}
