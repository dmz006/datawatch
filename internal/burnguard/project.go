package burnguard

import (
	"math"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// DefaultRateWindow is the trailing span the burn rate is measured over.
const DefaultRateWindow = 3 * time.Hour

// ProjectInput is everything Project reads.
type ProjectInput struct {
	Readings []usagecalc.Reading
	Reset    usagecalc.ResetSpec
	// Now is the moment of the projection. Readings after it are ignored by the caller.
	Now time.Time
	// Buckets and Through are the token data (see Usage). Through zero means Now.
	Buckets []Bucket
	Through time.Time
	Coef    Coefficients
	// RateWindow is the span the burn rate is measured over; 0 means DefaultRateWindow.
	RateWindow time.Duration
	// MarginPoints is how far under the ceiling the projection must stay; nil means
	// usagecalc.DefaultSafetyPoints.
	MarginPoints      *float64
	ExtraCreditPoints float64
}

// Projection is the result of Project. When Needs is set the readings or the reset were not
// usable and the numbers are zero.
type Projection struct {
	Needs string `json:"needs,omitempty"`
	// Estimated is true when a weekly reset passed since the newest reading: the base is
	// then 0 at that reset (ReadAt, PercentAtReading) and every point comes from the tokens.
	Estimated bool `json:"estimated,omitempty"`

	ReadAt    time.Time `json:"read_at"`
	NextReset time.Time `json:"next_reset"`

	// PercentAtReading is the newest reading; PointsSinceReading the points the tokens say
	// were used after it; EstimatedNow is their sum.
	PercentAtReading   float64 `json:"percent_at_reading"`
	PointsSinceReading float64 `json:"points_since_reading"`
	EstimatedNow       float64 `json:"estimated_now"`

	// PointsPerHour is the burn over the trailing window, all sessions; Worker and Other
	// split it. AllowedPerHour is the weekly pace (the allowance over the length of the week).
	PointsPerHour       float64 `json:"points_per_hour"`
	WorkerPointsPerHour float64 `json:"worker_points_per_hour"`
	OtherPointsPerHour  float64 `json:"other_points_per_hour"`
	AllowedPerHour      float64 `json:"allowed_per_hour"`
	// RemainingPerHour is what is left to the target spread over the hours to the reset;
	// negative when the target is already passed.
	RemainingPerHour float64 `json:"remaining_per_hour"`

	HoursLeft float64 `json:"hours_left"`
	// Projected is the percentage expected at the reset if the burn continues.
	Projected float64 `json:"projected"`
	// AdviceProjected is the percentage usagecalc's advice projects at the reset on the
	// readings alone, using its recent-burn projection (ProjectedEndRecent). It is the
	// readings-only counterpart of Projected, which adds the token-priced burn.
	AdviceProjected float64 `json:"advice_projected"`
	// Line is the pace line at the newest reading (usagecalc's PaceLine); LineNow is the
	// same line at Now.
	Line    float64 `json:"line"`
	LineNow float64 `json:"line_now"`
	Ceiling float64 `json:"ceiling"`
	Margin  float64 `json:"margin"`
	// Target is Ceiling minus Margin.
	Target float64 `json:"target"`
	// Over is true when Projected is above Target; OverBy is Projected minus Target, and
	// negative when under.
	Over   bool    `json:"over"`
	OverBy float64 `json:"over_by"`
	// AheadOfPace is true when EstimatedNow is above LineNow.
	AheadOfPace bool `json:"ahead_of_pace"`

	// ExhaustAt is when the ceiling is reached at this burn, set only when that is before
	// the reset; HoursEarly is how long before.
	ExhaustAt  *time.Time `json:"exhaust_at,omitempty"`
	HoursEarly float64    `json:"hours_early"`

	// AdviceVerdict is usagecalc's verdict on the readings alone (the recent burn between
	// readings), for comparison with the token-based projection.
	AdviceVerdict string `json:"advice_verdict"`

	// CalibrationRatio scales the token estimate to the readings (1 when not calibrated).
	// ReadingPointsPerHour and TokenPointsPerHour are the two rates PointsPerHour blends; zero
	// when that part could not be measured.
	CalibrationRatio     float64 `json:"calibration_ratio"`
	ReadingPointsPerHour float64 `json:"reading_points_per_hour"`
	TokenPointsPerHour   float64 `json:"token_points_per_hour"`
}

// EstimateWeek estimates the week that holds in.Now from the tokens alone, for a week with
// no reading yet: the points since its reset and the burn over the trailing window, both
// priced with Coef and scaled by the calibration ratio of the earlier readings (also
// returned). ok is false when there is nothing to estimate from: no token data, no prices
// or no usable reset.
func EstimateWeek(in ProjectInput) (est usagecalc.WeekEstimate, ratio float64, ok bool) {
	last, err := in.Reset.LastReset(in.Now)
	if err != nil || len(in.Buckets) == 0 || in.Coef == (Coefficients{}) {
		return est, 0, false
	}
	window, through := in.window(), in.through()
	// Only readings the token data reaches back to can calibrate: an older interval would
	// count its points against no tokens.
	var covered []usagecalc.Reading
	for _, r := range readingsAtOrBefore(in.Readings, in.Now) {
		if !r.At.Before(in.Buckets[0].Hour) {
			covered = append(covered, r)
		}
	}
	ratio = calibrationRatio(in.Buckets, covered, through, in.Coef)
	est.Points = price(tokensBetween(in.Buckets, last, in.Now, through), in.Coef) * ratio
	w, o := splitPoints(in.Buckets, in.Now.Add(-window), in.Now, through, in.Coef)
	est.PointsPerHour = (w + o) / window.Hours() * ratio
	return est, ratio, true
}

// window is the span the burn rate is measured over.
func (in ProjectInput) window() time.Duration {
	if in.RateWindow <= 0 {
		return DefaultRateWindow
	}
	return in.RateWindow
}

// through is where the token data ends, at the latest Now.
func (in ProjectInput) through() time.Time {
	if in.Through.IsZero() || in.Through.After(in.Now) {
		return in.Now
	}
	return in.Through
}

// estimatedProjection is the projection of a week with no reading yet. There is no
// readings-only projection of that week, so AdviceProjected repeats Projected and the
// guard's gap check never fires on it.
func estimatedProjection(in ProjectInput, adv usagecalc.Advice, est usagecalc.WeekEstimate, ratio, margin float64) Projection {
	last, next := *adv.LastResetAt, *adv.NextResetAt
	p := Projection{
		Estimated: true, ReadAt: last, NextReset: next,
		PointsSinceReading: est.Points, EstimatedNow: est.Points,
		Ceiling: adv.Ceiling, Margin: margin, Target: adv.Ceiling - margin,
		AllowedPerHour: adv.AllowancePer24h / 24, AdviceVerdict: adv.Verdict,
		CalibrationRatio: ratio, TokenPointsPerHour: est.PointsPerHour, PointsPerHour: est.PointsPerHour,
		HoursLeft: next.Sub(in.Now).Hours(),
	}
	if week := next.Sub(last).Hours(); week > 0 {
		p.LineNow = in.Now.Sub(last).Hours() / week * 100
	}
	window := in.window()
	w, o := splitPoints(in.Buckets, in.Now.Add(-window), in.Now, in.through(), in.Coef)
	p.WorkerPointsPerHour, p.OtherPointsPerHour = w/window.Hours(), o/window.Hours()
	p.Projected = p.EstimatedNow + p.PointsPerHour*p.HoursLeft
	p.AdviceProjected = p.Projected
	finish(&p, in.Now)
	return p
}

// Project projects the points used to the weekly reset and compares them with the pace line.
// The newest percentage reading is the base; the tokens after it, priced with Coef, bring it
// up to Now; the burn over the trailing window carries it on to the reset. When a weekly
// reset has passed since the newest reading, the base is 0 at that reset (see EstimateWeek).
func Project(in ProjectInput) Projection {
	margin := usagecalc.DefaultSafetyPoints
	if in.MarginPoints != nil && !math.IsNaN(*in.MarginPoints) && *in.MarginPoints >= 0 {
		margin = *in.MarginPoints
	}
	window, through := in.window(), in.through()
	var sinceReset *usagecalc.WeekEstimate
	var ratio float64
	if usagecalc.ResetPassed(in.Readings, in.Reset, in.Now) {
		if est, r, ok := EstimateWeek(in); ok {
			sinceReset, ratio = &est, r
		}
	}
	adv := usagecalc.Advise(usagecalc.Input{
		Readings: in.Readings, Reset: in.Reset, Now: in.Now,
		ExtraCreditPoints: in.ExtraCreditPoints, SafetyPoints: &margin, SinceReset: sinceReset,
	})
	if adv.Needs != "" {
		return Projection{Needs: adv.Needs}
	}
	if adv.Estimated {
		return estimatedProjection(in, adv, *sinceReset, ratio, margin)
	}
	readAt, next := *adv.ReadAt, *adv.NextResetAt
	if in.Now.Before(readAt) {
		// The newest reading is in the future.
		return Projection{Needs: usagecalc.NeedsReading}
	}

	p := Projection{
		ReadAt: readAt, NextReset: next,
		PercentAtReading: adv.Percent, Ceiling: adv.Ceiling, Margin: margin,
		Line: adv.PaceLine, AllowedPerHour: adv.AllowancePer24h / 24,
		AdviceVerdict: adv.Verdict,
	}
	p.Target = p.Ceiling - margin
	p.AdviceProjected = adv.ProjectedEndRecent

	cal := ComputeCalibratedBurn(in.Buckets, in.Readings, readAt, in.Now, through, in.Coef)
	p.CalibrationRatio = cal.Ratio
	if cal.HasReadingRate {
		p.ReadingPointsPerHour = cal.ReadingRate
	}
	if cal.HasTokenRate {
		p.TokenPointsPerHour = cal.TokenRate
	}
	since := tokensBetween(in.Buckets, readAt, in.Now, through)
	p.PointsSinceReading = price(since, in.Coef) * cal.Ratio
	p.EstimatedNow = p.PercentAtReading + p.PointsSinceReading

	last, _ := in.Reset.LastReset(in.Now)
	if week := next.Sub(last).Hours(); week > 0 {
		p.LineNow = in.Now.Sub(last).Hours() / week * 100
	}
	p.AheadOfPace = p.EstimatedNow > p.LineNow

	win := in.Now.Sub(in.Now.Add(-window)).Hours()
	w, o := splitPoints(in.Buckets, in.Now.Add(-window), in.Now, through, in.Coef)
	p.WorkerPointsPerHour, p.OtherPointsPerHour = w/win, o/win
	p.PointsPerHour = p.WorkerPointsPerHour + p.OtherPointsPerHour
	if cal.Available {
		p.PointsPerHour = cal.Rate
	}

	p.HoursLeft = next.Sub(in.Now).Hours()
	p.Projected = p.EstimatedNow + p.PointsPerHour*p.HoursLeft
	p.OverBy = p.Projected - p.Target
	p.Over = p.Projected > p.Target
	if p.HoursLeft > 0 {
		p.RemainingPerHour = (p.Target - p.EstimatedNow) / p.HoursLeft
	}
	if p.PointsPerHour > 0 {
		h := math.Max((p.Ceiling-p.EstimatedNow)/p.PointsPerHour, 0)
		if h < p.HoursLeft {
			at := in.Now.Add(time.Duration(h * float64(time.Hour)))
			p.ExhaustAt, p.HoursEarly = &at, p.HoursLeft-h
		}
	}
	return p
}

// splitPoints prices the worker and the other tokens in [from, to) separately.
func splitPoints(buckets []Bucket, from, to, through time.Time, c Coefficients) (worker, other float64) {
	ws := make([]Bucket, len(buckets))
	os := make([]Bucket, len(buckets))
	for i, b := range buckets {
		ws[i] = Bucket{Hour: b.Hour, Other: b.Worker}
		os[i] = Bucket{Hour: b.Hour, Other: b.Other}
	}
	price := func(bs []Bucket) float64 {
		return price(tokensBetween(bs, from, to, through), c)
	}
	return price(ws), price(os)
}
