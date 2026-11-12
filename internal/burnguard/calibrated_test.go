package burnguard

import (
	"math"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// calT0 is the start of every calibration test's timeline.
var calT0 = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

func calPct(at time.Time, p float64) usagecalc.Reading {
	v := p
	return usagecalc.Reading{At: at, Percent: &v}
}

// calWriteOnly prices one million cache-write tokens at one point each.
var calWriteOnly = Coefficients{CacheWritePerM: 1}

func calBucket(hour time.Time, cacheWrite int64) Bucket {
	return Bucket{Hour: hour, Other: Tokens{CacheWrite: cacheWrite}}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCalibratedReadingRateOnly(t *testing.T) {
	// Two readings an hour apart rise 1 point, and each hour's million cache writes price at
	// one point, so the ratio is 1. The newest reading is now, so there is no token rate.
	readings := []usagecalc.Reading{calPct(calT0, 10), calPct(calT0.Add(time.Hour), 11), calPct(calT0.Add(2*time.Hour), 12)}
	buckets := []Bucket{calBucket(calT0, 1e6), calBucket(calT0.Add(time.Hour), 1e6)}
	now := calT0.Add(2 * time.Hour)
	c := ComputeCalibratedBurn(buckets, readings, now, now, now, calWriteOnly)
	if !approx(c.Ratio, 1) || !c.HasReadingRate || c.HasTokenRate || !c.Available {
		t.Fatalf("got %+v", c)
	}
	if !approx(c.ReadingRate, 1) || !approx(c.Rate, 1) {
		t.Fatalf("reading rate %.3f rate %.3f, want 1", c.ReadingRate, c.Rate)
	}
}

func TestCalibratedBlendsReadingAndTokenRates(t *testing.T) {
	// The reading rate is 1 point an hour. Since the newest reading (at +2h) the tokens price
	// at 2 points over one hour, so the token rate is 2 and the blend is their mean. The reading
	// at +4h is after now and must be ignored.
	readings := []usagecalc.Reading{
		calPct(calT0, 10), calPct(calT0.Add(time.Hour), 11), calPct(calT0.Add(2*time.Hour), 12),
		calPct(calT0.Add(4*time.Hour), 99),
	}
	buckets := []Bucket{
		calBucket(calT0, 1e6), calBucket(calT0.Add(time.Hour), 1e6), calBucket(calT0.Add(2*time.Hour), 2e6),
	}
	now := calT0.Add(3 * time.Hour)
	c := ComputeCalibratedBurn(buckets, readings, calT0.Add(2*time.Hour), now, now, calWriteOnly)
	if !approx(c.Ratio, 1) || !approx(c.ReadingRate, 1) || !approx(c.TokenRate, 2) {
		t.Fatalf("got %+v", c)
	}
	if !approx(c.Rate, RateBlendReadingWeight*1+(1-RateBlendReadingWeight)*2) {
		t.Fatalf("blend %.3f", c.Rate)
	}
}

func TestCalibratedRatioClampsHigh(t *testing.T) {
	// Readings rise 10 and 10 over intervals that price at one point each: ratio 10, clamped.
	readings := []usagecalc.Reading{calPct(calT0, 0), calPct(calT0.Add(time.Hour), 10), calPct(calT0.Add(2*time.Hour), 20)}
	buckets := []Bucket{calBucket(calT0, 1e6), calBucket(calT0.Add(time.Hour), 1e6)}
	now := calT0.Add(2 * time.Hour)
	c := ComputeCalibratedBurn(buckets, readings, now, now, now, calWriteOnly)
	if !approx(c.Ratio, CalibrationMaxRatio) {
		t.Fatalf("ratio %.3f, want clamp %.1f", c.Ratio, CalibrationMaxRatio)
	}
}

func TestCalibratedRatioClampsLow(t *testing.T) {
	// Readings rise 0.1 over two intervals that price at one point each: ratio 0.05, clamped.
	readings := []usagecalc.Reading{calPct(calT0, 0), calPct(calT0.Add(time.Hour), 0.05), calPct(calT0.Add(2*time.Hour), 0.1)}
	buckets := []Bucket{calBucket(calT0, 1e6), calBucket(calT0.Add(time.Hour), 1e6)}
	now := calT0.Add(2 * time.Hour)
	c := ComputeCalibratedBurn(buckets, readings, now, now, now, calWriteOnly)
	if !approx(c.Ratio, CalibrationMinRatio) {
		t.Fatalf("ratio %.3f, want clamp %.1f", c.Ratio, CalibrationMinRatio)
	}
}

func TestCalibratedRatioUsesNewestIntervals(t *testing.T) {
	// Seven intervals: the first one rises 100 points with no tokens and must be left out of
	// the last six. The last six rise 1 point each over 1 estimated point each: ratio 1.
	readings := []usagecalc.Reading{calPct(calT0, 0)}
	buckets := []Bucket{}
	for i := 1; i <= 7; i++ {
		at := calT0.Add(time.Duration(i) * time.Hour)
		// Cumulative: interval 1 rises 100, intervals 2..7 rise 1 each.
		p := 100 + float64(i-1)
		readings = append(readings, calPct(at, p))
		buckets = append(buckets, calBucket(at.Add(-time.Hour), 1e6))
	}
	now := calT0.Add(7 * time.Hour)
	c := ComputeCalibratedBurn(buckets, readings, now, now, now, calWriteOnly)
	if !approx(c.Ratio, 1) {
		t.Fatalf("ratio %.3f, want 1 from the newest six intervals", c.Ratio)
	}
}

func TestCalibratedRatioFallsBackToOne(t *testing.T) {
	readings := []usagecalc.Reading{calPct(calT0, 0), calPct(calT0.Add(time.Hour), 5)}
	buckets := []Bucket{calBucket(calT0, 1e6)}
	now := calT0.Add(time.Hour)
	// One interval only.
	if c := ComputeCalibratedBurn(buckets, readings, now, now, now, calWriteOnly); c.Ratio != 1 {
		t.Fatalf("one interval: ratio %.3f", c.Ratio)
	}
	// Two intervals but a zero estimate (no coefficient).
	readings = append(readings, calPct(calT0.Add(2*time.Hour), 9))
	now = calT0.Add(2 * time.Hour)
	if c := ComputeCalibratedBurn(buckets, readings, now, now, now, Coefficients{}); c.Ratio != 1 {
		t.Fatalf("zero estimate: ratio %.3f", c.Ratio)
	}
}

func TestCalibratedReadingRateUnavailable(t *testing.T) {
	now := calT0.Add(2 * time.Hour)
	buckets := []Bucket{calBucket(calT0.Add(time.Hour), 1e6)}
	// A fall between the last two readings is a reset; the reading rate is not measured and
	// the token rate since the newest reading (at +1h) carries the burn alone.
	falls := []usagecalc.Reading{calPct(calT0, 50), calPct(calT0.Add(time.Hour), 10)}
	c := ComputeCalibratedBurn(buckets, falls, calT0.Add(time.Hour), now, now, calWriteOnly)
	if c.HasReadingRate || !c.HasTokenRate || !approx(c.Rate, c.TokenRate) || !approx(c.TokenRate, 1) {
		t.Fatalf("fall: got %+v", c)
	}
	// Readings closer than MinIntervalMinutes are too short to use.
	short := []usagecalc.Reading{calPct(now.Add(-5*time.Minute), 10), calPct(now, 11)}
	if c := ComputeCalibratedBurn(nil, short, now, now, now, calWriteOnly); c.HasReadingRate {
		t.Fatalf("short gap: got %+v", c)
	}
	// Nothing measurable: no reading pair and the newest reading is now.
	if c := ComputeCalibratedBurn(nil, []usagecalc.Reading{calPct(now, 10)}, now, now, now, calWriteOnly); c.Available || c.Rate != 0 {
		t.Fatalf("nothing measurable: got %+v", c)
	}
}
