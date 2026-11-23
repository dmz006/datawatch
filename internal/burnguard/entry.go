package burnguard

import (
	"math"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// The guard evaluates once per paid LLM entry. Each entry has its own readings (the
// readings that name it), its own plan type, period target, ladder and held models, and
// its tokens are priced with its model's own cost rate. Nothing here names a backend:
// every name comes from the entry.

// NeedsTarget is the Needs of a metered entry with no period target in dollars.
const NeedsTarget = "period_target"

// ModelRate is a model's own cost rate in dollars per 1K tokens (the per-model rate of
// the LLM entry configuration).
type ModelRate struct {
	InPerK         float64 `json:"in_per_k"`
	OutPerK        float64 `json:"out_per_k"`
	CacheReadPerK  float64 `json:"cache_read_per_k"`
	CacheWritePerK float64 `json:"cache_write_per_k"`
}

// IsZero reports whether the rate prices nothing.
func (r ModelRate) IsZero() bool {
	return r.InPerK == 0 && r.OutPerK == 0 && r.CacheReadPerK == 0 && r.CacheWritePerK == 0
}

// CoefficientsFromRate turns a model rate into token coefficients in the entry's unit:
// unitsPerUSD is 1 for a metered entry (dollars) and the points per dollar of a flat
// entry's allowance.
func CoefficientsFromRate(r ModelRate, unitsPerUSD float64) Coefficients {
	k := 1000 * unitsPerUSD
	return Coefficients{
		CacheWritePerM: r.CacheWritePerK * k, CacheReadPerM: r.CacheReadPerK * k,
		InputPerM: r.InPerK * k, OutputPerM: r.OutPerK * k,
	}
}

// EntryProjectInput is one entry's projection input. ProjectInput.Readings must already be
// the entry's own readings (usagecalc.ReadingsForEntry).
type EntryProjectInput struct {
	ProjectInput
	// Metered selects the spend projection; false is the flat (percent) projection.
	Metered bool
	// PeriodTarget is the period ceiling: dollars for a metered entry (required), a
	// percentage for a flat entry (0 means the weekly allowance, 100).
	PeriodTarget float64
}

// ProjectEntry projects one entry: a flat entry from its percentage readings (Project,
// moved to the entry's target), a metered entry from its spend readings (ProjectMetered).
func ProjectEntry(in EntryProjectInput) Projection {
	if in.Metered {
		return ProjectMetered(in.ProjectInput, in.PeriodTarget)
	}
	p := Project(in.ProjectInput)
	if p.Needs == "" && in.PeriodTarget > 0 && in.PeriodTarget != usagecalc.WeeklyAllowance {
		retarget(&p, in.PeriodTarget+in.ExtraCreditPoints, in.Now)
	}
	return p
}

// retarget moves a projection to a new ceiling (keeping the margin) and recomputes what
// depends on it.
func retarget(p *Projection, ceiling float64, now time.Time) {
	if p.Ceiling > 0 {
		scale := ceiling / p.Ceiling
		p.Line *= scale
		p.LineNow *= scale
		p.AllowedPerHour *= scale
	}
	p.Ceiling = ceiling
	p.Target = ceiling - p.Margin
	finish(p, now)
}

// finish computes the comparisons from EstimatedNow, PointsPerHour, HoursLeft, Projected,
// Ceiling and Target.
func finish(p *Projection, now time.Time) {
	p.OverBy = p.Projected - p.Target
	p.Over = p.Projected > p.Target
	p.AheadOfPace = p.EstimatedNow > p.LineNow
	p.RemainingPerHour = 0
	if p.HoursLeft > 0 {
		p.RemainingPerHour = (p.Target - p.EstimatedNow) / p.HoursLeft
	}
	p.ExhaustAt, p.HoursEarly = nil, 0
	if p.PointsPerHour > 0 {
		h := math.Max((p.Ceiling-p.EstimatedNow)/p.PointsPerHour, 0)
		if h < p.HoursLeft {
			at := now.Add(time.Duration(h * float64(time.Hour)))
			p.ExhaustAt, p.HoursEarly = &at, p.HoursLeft-h
		}
	}
}

// ProjectMetered projects a metered entry's spend to the end of the period, in dollars.
// The newest spend reading of the period is the base; the tokens after it, priced with
// Coef (the model's own rate, CoefficientsFromRate with 1), bring it up to Now; the burn
// carries it on to the reset. The burn is the priced tokens over the trailing window, or
// when there are no tokens the spend between the period's readings. The margin is a
// percentage of the target (MarginPoints, default usagecalc.DefaultSafetyPoints). All
// projection fields are in dollars (PercentAtReading is the spend at the reading).
// AdviceProjected repeats Projected: a metered entry has no readings-only advice, so the
// guard/advice gap check never fires on it.
func ProjectMetered(in ProjectInput, target float64) Projection {
	if !(target > 0) || math.IsInf(target, 0) {
		return Projection{Needs: NeedsTarget}
	}
	last, err := in.Reset.LastReset(in.Now)
	if err != nil {
		return Projection{Needs: usagecalc.NeedsReset}
	}
	next, err := in.Reset.NextReset(in.Now)
	if err != nil {
		return Projection{Needs: usagecalc.NeedsReset}
	}
	var spend []usagecalc.Reading
	for _, r := range in.Readings {
		if r.SpendUSD != nil && !r.At.Before(last) && !r.At.After(in.Now) {
			spend = append(spend, r)
		}
	}
	if len(spend) == 0 {
		return Projection{Needs: usagecalc.NeedsReading}
	}
	newest, oldest := spend[0], spend[0]
	for _, r := range spend {
		if r.At.After(newest.At) {
			newest = r
		}
		if r.At.Before(oldest.At) {
			oldest = r
		}
	}
	marginPct := usagecalc.DefaultSafetyPoints
	if in.MarginPoints != nil && !math.IsNaN(*in.MarginPoints) && *in.MarginPoints >= 0 {
		marginPct = *in.MarginPoints
	}
	window := in.RateWindow
	if window <= 0 {
		window = DefaultRateWindow
	}
	through := in.Through
	if through.IsZero() || through.After(in.Now) {
		through = in.Now
	}

	p := Projection{ReadAt: newest.At, NextReset: next, PercentAtReading: *newest.SpendUSD,
		Ceiling: target, Margin: target * marginPct / 100, CalibrationRatio: 1}
	p.Target = p.Ceiling - p.Margin
	period := next.Sub(last).Hours()
	if period > 0 {
		p.AllowedPerHour = target / period
		p.Line = newest.At.Sub(last).Hours() / period * target
		p.LineNow = in.Now.Sub(last).Hours() / period * target
	}
	p.PointsSinceReading = price(tokensBetween(in.Buckets, newest.At, in.Now, through), in.Coef)
	p.EstimatedNow = p.PercentAtReading + p.PointsSinceReading

	if h := window.Hours(); h > 0 {
		p.TokenPointsPerHour = price(tokensBetween(in.Buckets, in.Now.Add(-window), in.Now, through), in.Coef) / h
	}
	switch {
	case newest.At.Sub(oldest.At) >= time.Hour:
		p.ReadingPointsPerHour = math.Max(*newest.SpendUSD-*oldest.SpendUSD, 0) / newest.At.Sub(oldest.At).Hours()
	case newest.At.Sub(last) >= time.Hour:
		// One reading in the period: the spend since the reset is the burn.
		p.ReadingPointsPerHour = *newest.SpendUSD / newest.At.Sub(last).Hours()
	}
	p.PointsPerHour = p.ReadingPointsPerHour
	if p.TokenPointsPerHour > 0 {
		p.PointsPerHour = p.TokenPointsPerHour
	}
	p.HoursLeft = next.Sub(in.Now).Hours()
	p.Projected = p.EstimatedNow + p.PointsPerHour*p.HoursLeft
	p.AdviceProjected = p.Projected
	finish(&p, in.Now)
	return p
}
