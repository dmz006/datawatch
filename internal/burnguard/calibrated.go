package burnguard

import (
	"math"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// Calibrated burn rate. The token estimate of the points used is only as good as the fitted
// coefficients, so the burn is anchored to the operator's own percentage readings where it
// can be: the rate between the last two readings, and the token estimate since the newest one
// scaled by how far the readings have actually moved against the estimate.

// CalibrationIntervals is how many of the newest reading intervals the calibration ratio is
// measured over.
const CalibrationIntervals = 6

// CalibrationMinRatio and CalibrationMaxRatio clamp the calibration ratio so one noisy stretch
// of readings cannot scale the token estimate by an unreasonable factor.
const (
	CalibrationMinRatio = 0.5
	CalibrationMaxRatio = 2.0
)

// RateBlendReadingWeight is the weight of the reading rate in the blended burn; the token rate
// gets the rest. Equal weights.
const RateBlendReadingWeight = 0.5

// CalibratedBurn is the burn per hour from the readings and the tokens, and the parts it is
// made of. Available is false when neither part could be measured; the rates are then zero.
type CalibratedBurn struct {
	// Ratio is the calibration ratio: points the readings moved over the points the token
	// estimate gave for the same intervals, clamped to [CalibrationMinRatio, CalibrationMaxRatio].
	// It is 1 when there are fewer than two intervals or the estimate is zero.
	Ratio float64
	// ReadingRate is the points the readings moved between the last two readings, per hour.
	ReadingRate float64
	// TokenRate is the token estimate since the newest reading, times Ratio, per hour.
	TokenRate float64
	// Rate is the blend of ReadingRate and TokenRate; whichever one is available when only
	// one is.
	Rate      float64
	Available bool
	// HasReadingRate and HasTokenRate say which parts were measured.
	HasReadingRate bool
	HasTokenRate   bool
}

// ComputeCalibratedBurn works out the calibrated burn at now, from the newest reading readAt.
// Readings after now are ignored. through is where the token data ends (see Usage.Through).
func ComputeCalibratedBurn(buckets []Bucket, readings []usagecalc.Reading, readAt, now, through time.Time, coef Coefficients) CalibratedBurn {
	rs := percentReadingsUpTo(readings, now)
	out := CalibratedBurn{Ratio: calibrationRatio(buckets, readingsAtOrBefore(readings, now), through, coef)}
	out.ReadingRate, out.HasReadingRate = readingRate(rs)
	out.TokenRate, out.HasTokenRate = tokenRate(buckets, readAt, now, through, coef, out.Ratio)
	switch {
	case out.HasReadingRate && out.HasTokenRate:
		out.Rate = RateBlendReadingWeight*out.ReadingRate + (1-RateBlendReadingWeight)*out.TokenRate
		out.Available = true
	case out.HasReadingRate:
		out.Rate, out.Available = out.ReadingRate, true
	case out.HasTokenRate:
		out.Rate, out.Available = out.TokenRate, true
	}
	return out
}

// calibrationRatio is the points the readings moved over the token estimate of the same
// intervals, across the newest CalibrationIntervals of them.
func calibrationRatio(buckets []Bucket, readings []usagecalc.Reading, through time.Time, coef Coefficients) float64 {
	ivs := Intervals(buckets, readings, through)
	if len(ivs) < 2 {
		return 1
	}
	if len(ivs) > CalibrationIntervals {
		ivs = ivs[len(ivs)-CalibrationIntervals:]
	}
	var points, estimate float64
	for _, iv := range ivs {
		points += iv.Points
		// The same pricing as Coefficients.Points, per million tokens.
		estimate += iv.CacheWriteM*coef.CacheWritePerM + iv.CacheReadM*coef.CacheReadPerM
	}
	if estimate == 0 || math.IsNaN(estimate) {
		return 1
	}
	return math.Min(math.Max(points/estimate, CalibrationMinRatio), CalibrationMaxRatio)
}

// readingRate is the points that moved between the last two readings, per hour. It is not
// measured when the percentage fell (a reset between them) or the readings are closer than
// MinIntervalMinutes, which are too short to resolve.
func readingRate(rs []pctReading) (float64, bool) {
	if len(rs) < 2 {
		return 0, false
	}
	a, b := rs[len(rs)-2], rs[len(rs)-1]
	span := b.at.Sub(a.at)
	if b.p < a.p || span < MinIntervalMinutes*time.Minute {
		return 0, false
	}
	return (b.p - a.p) / span.Hours(), true
}

// tokenRate is the token estimate since the newest reading, scaled by ratio, per hour.
func tokenRate(buckets []Bucket, readAt, now, through time.Time, coef Coefficients, ratio float64) (float64, bool) {
	hours := now.Sub(readAt).Hours()
	if hours <= 0 {
		return 0, false
	}
	t := tokensBetween(buckets, readAt, now, through)
	points := t.cw/1e6*coef.CacheWritePerM + t.cr/1e6*coef.CacheReadPerM
	return points * ratio / hours, true
}

// pctReading is a usable percentage reading.
type pctReading struct {
	at time.Time
	p  float64
}

// readingsAtOrBefore returns the readings at or before now.
func readingsAtOrBefore(readings []usagecalc.Reading, now time.Time) []usagecalc.Reading {
	var out []usagecalc.Reading
	for _, r := range readings {
		if !r.At.After(now) {
			out = append(out, r)
		}
	}
	return out
}

// percentReadingsUpTo returns the usable percentage readings at or before now, oldest first.
func percentReadingsUpTo(readings []usagecalc.Reading, now time.Time) []pctReading {
	var out []pctReading
	for _, r := range readingsAtOrBefore(readings, now) {
		if r.Percent == nil || r.At.IsZero() || math.IsNaN(*r.Percent) || math.IsInf(*r.Percent, 0) {
			continue
		}
		out = append(out, pctReading{r.At, *r.Percent})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}
