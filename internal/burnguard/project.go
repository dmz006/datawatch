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

// Project projects the points used to the weekly reset and compares them with the pace line.
// The newest percentage reading is the base; the tokens after it, priced with Coef, bring it
// up to Now; the burn over the trailing window carries it on to the reset.
func Project(in ProjectInput) Projection {
	margin := usagecalc.DefaultSafetyPoints
	if in.MarginPoints != nil && !math.IsNaN(*in.MarginPoints) && *in.MarginPoints >= 0 {
		margin = *in.MarginPoints
	}
	window := in.RateWindow
	if window <= 0 {
		window = DefaultRateWindow
	}
	through := in.Through
	if through.IsZero() || through.After(in.Now) {
		through = in.Now
	}
	adv := usagecalc.Advise(usagecalc.Input{
		Readings: in.Readings, Reset: in.Reset, Now: in.Now,
		ExtraCreditPoints: in.ExtraCreditPoints, SafetyPoints: &margin,
	})
	if adv.Needs != "" {
		return Projection{Needs: adv.Needs}
	}
	readAt, next := *adv.ReadAt, *adv.NextResetAt
	if !in.Now.Before(next) || in.Now.Before(readAt) {
		// The newest reading is from before the last reset, or is in the future.
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
	p.PointsSinceReading = (since.cw/1e6*in.Coef.CacheWritePerM + since.cr/1e6*in.Coef.CacheReadPerM) * cal.Ratio
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
		s := tokensBetween(bs, from, to, through)
		return s.cw/1e6*c.CacheWritePerM + s.cr/1e6*c.CacheReadPerM
	}
	return price(ws), price(os)
}
