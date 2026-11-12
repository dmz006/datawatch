package burnguard

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// Coefficients convert tokens to points of the weekly allowance. Output and uncached input
// tokens correlate with cache writes and add nothing once those are in, so only the two
// cache kinds carry a coefficient.
type Coefficients struct {
	// CacheWritePerM is points per million cache-write tokens.
	CacheWritePerM float64 `json:"cache_write_per_million"`
	// CacheReadPerM is points per million cache-read tokens.
	CacheReadPerM float64 `json:"cache_read_per_million"`
}

// DefaultCoefficients are the values fitted to the operator's readings of 2026-10-25 to
// 2026-10-31.
var DefaultCoefficients = Coefficients{CacheWritePerM: 0.373, CacheReadPerM: 0.0069}

// Points is the estimated points of the allowance that t used.
func (c Coefficients) Points(t Tokens) float64 {
	return float64(t.CacheWrite)/1e6*c.CacheWritePerM + float64(t.CacheRead)/1e6*c.CacheReadPerM
}

// Fit limits.
const (
	// MinFitIntervals is the fewest reading intervals a refit is attempted with; two
	// coefficients need more than two equations to say anything about the error.
	MinFitIntervals = 4
	// MinIntervalMinutes drops intervals too short for the hourly buckets to resolve.
	MinIntervalMinutes = 15
)

// Interval is the span between two consecutive percentage readings with the points the
// operator saw the percentage rise by and the tokens the transcripts recorded in it.
type Interval struct {
	From, To time.Time
	// Points is the rise in the weekly percentage.
	Points float64
	// CacheWriteM and CacheReadM are the tokens in the span, in millions.
	CacheWriteM, CacheReadM float64
}

// FitResult is the outcome of a refit.
type FitResult struct {
	// Coef is the coefficients to use: the refitted ones when Refit is true, otherwise the
	// prior ones.
	Coef Coefficients `json:"coef"`
	// Prior is what the fit started from.
	Prior Coefficients `json:"prior"`
	// Refit says the coefficients were replaced by the least-squares solution.
	Refit bool `json:"refit"`
	// Reason says why a refit was not applied; empty when Refit is true.
	Reason string `json:"reason,omitempty"`
	// Intervals is the number of intervals the fit used.
	Intervals int `json:"intervals"`
	// RMSE and MaxAbsErr are the error of Coef over those intervals, in points.
	RMSE      float64 `json:"rmse"`
	MaxAbsErr float64 `json:"max_abs_err"`
	// PriorRMSE is the error of Prior over the same intervals, so the gain is visible.
	PriorRMSE float64 `json:"prior_rmse"`
}

// Intervals pairs consecutive percentage readings and sums the tokens inside each. A pair
// where the percentage fell (a weekly reset between them) or that is shorter than
// MinIntervalMinutes is dropped. through is where the token data ends (Usage.Through); the
// zero time means the buckets are complete hours.
func Intervals(buckets []Bucket, readings []usagecalc.Reading, through time.Time) []Interval {
	type pr struct {
		at time.Time
		p  float64
	}
	var rs []pr
	for _, r := range readings {
		if r.Percent == nil || r.At.IsZero() || math.IsNaN(*r.Percent) || math.IsInf(*r.Percent, 0) {
			continue
		}
		rs = append(rs, pr{r.At, *r.Percent})
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].at.Before(rs[j].at) })
	var out []Interval
	for i := 1; i < len(rs); i++ {
		a, b := rs[i-1], rs[i]
		if b.p < a.p || b.at.Sub(a.at) < MinIntervalMinutes*time.Minute {
			continue
		}
		t := tokensBetween(buckets, a.at, b.at, through)
		out = append(out, Interval{From: a.at, To: b.at, Points: b.p - a.p,
			CacheWriteM: t.cw / 1e6, CacheReadM: t.cr / 1e6})
	}
	return out
}

type tokenSums struct{ cw, cr float64 }

// tokensBetween sums the cache tokens in [from, to). A bucket partly inside the span counts
// in proportion to the overlap, as if its tokens were spread evenly over the hour. The hour
// that holds through is only as long as the data in it.
func tokensBetween(buckets []Bucket, from, to, through time.Time) tokenSums {
	var s tokenSums
	if !to.After(from) {
		return s
	}
	for _, b := range buckets {
		start, end := b.Hour, b.Hour.Add(time.Hour)
		if !through.IsZero() && through.Before(end) {
			end = through
		}
		if !end.After(start) {
			continue
		}
		lo, hi := maxTime(start, from), minTime(end, to)
		if !hi.After(lo) {
			continue
		}
		frac := float64(hi.Sub(lo)) / float64(end.Sub(start))
		t := b.Total()
		s.cw += float64(t.CacheWrite) * frac
		s.cr += float64(t.CacheRead) * frac
	}
	return s
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// errorOf is the root-mean-square and the largest absolute error of c over the intervals.
func errorOf(c Coefficients, ivs []Interval) (rmse, maxAbs float64) {
	if len(ivs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, iv := range ivs {
		e := iv.CacheWriteM*c.CacheWritePerM + iv.CacheReadM*c.CacheReadPerM - iv.Points
		sum += e * e
		maxAbs = math.Max(maxAbs, math.Abs(e))
	}
	return math.Sqrt(sum / float64(len(ivs))), maxAbs
}

// Refit fits the two coefficients to the intervals by least squares, with no intercept:
// points = a*cacheWriteM + b*cacheReadM. When the data cannot support a fit (too few
// intervals, collinear columns, or a negative coefficient, which no token kind can have) the
// prior coefficients are kept and Reason says why. The error fields describe the coefficients
// returned, so the caller sees the fit error either way.
func Refit(buckets []Bucket, readings []usagecalc.Reading, prior Coefficients, through time.Time) FitResult {
	ivs := Intervals(buckets, readings, through)
	res := FitResult{Coef: prior, Prior: prior, Intervals: len(ivs)}
	res.PriorRMSE, _ = errorOf(prior, ivs)
	res.RMSE, res.MaxAbsErr = errorOf(prior, ivs)
	if len(ivs) < MinFitIntervals {
		res.Reason = "too few reading intervals"
		return res
	}
	var sxx, sxy, syy, sxd, syd float64
	for _, iv := range ivs {
		sxx += iv.CacheWriteM * iv.CacheWriteM
		sxy += iv.CacheWriteM * iv.CacheReadM
		syy += iv.CacheReadM * iv.CacheReadM
		sxd += iv.CacheWriteM * iv.Points
		syd += iv.CacheReadM * iv.Points
	}
	det := sxx*syy - sxy*sxy
	if sxx == 0 || syy == 0 || det <= 1e-9*sxx*syy {
		res.Reason = "cache-write and cache-read tokens are too alike to separate"
		return res
	}
	a := (sxd*syy - syd*sxy) / det
	b := (syd*sxx - sxd*sxy) / det
	if math.IsNaN(a) || math.IsNaN(b) || a <= 0 || b < 0 {
		res.Reason = "least squares gave a non-positive coefficient"
		return res
	}
	res.Coef = Coefficients{CacheWritePerM: a, CacheReadPerM: b}
	res.Refit = true
	res.RMSE, res.MaxAbsErr = errorOf(res.Coef, ivs)
	return res
}

// Fitter keeps the current coefficients and refits them when a newer percentage reading
// arrives. It is safe for concurrent use.
type Fitter struct {
	mu     sync.Mutex
	coef   Coefficients
	lastAt time.Time
	last   FitResult
}

// NewFitter starts with the given coefficients, DefaultCoefficients if the zero value is passed.
func NewFitter(c Coefficients) *Fitter {
	if c == (Coefficients{}) {
		c = DefaultCoefficients
	}
	return &Fitter{coef: c, last: FitResult{Coef: c, Prior: c}}
}

// Coefficients returns the coefficients in force.
func (f *Fitter) Coefficients() Coefficients {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.coef
}

// Last returns the most recent fit result.
func (f *Fitter) Last() FitResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// Observe refits when the newest percentage reading is newer than the one last fitted
// against, and reports whether it did. A refit that is rejected still records the reading,
// so the same data is not fitted again on every call.
func (f *Fitter) Observe(buckets []Bucket, readings []usagecalc.Reading, through time.Time) (FitResult, bool) {
	var newest time.Time
	for _, r := range readings {
		if r.Percent != nil && r.At.After(newest) {
			newest = r.At
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if newest.IsZero() || !newest.After(f.lastAt) {
		return f.last, false
	}
	res := Refit(buckets, readings, f.coef, through)
	f.lastAt = newest
	f.last = res
	if res.Refit {
		f.coef = res.Coef
	}
	return res, true
}
