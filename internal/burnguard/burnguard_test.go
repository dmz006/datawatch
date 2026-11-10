package burnguard

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

type fixture struct {
	Reset        usagecalc.ResetSpec `json:"reset"`
	Recorded     map[string]any      `json:"recorded"`
	Coefficients Coefficients        `json:"coefficients"`
	Through      time.Time           `json:"through"`
	Readings     []usagecalc.Reading `json:"readings"`
	Buckets      []Bucket            `json:"buckets"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "burnguard", "friday-night.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func ny(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	return loc
}

// readingsUpTo keeps the readings the operator had at now.
func readingsUpTo(rs []usagecalc.Reading, now time.Time) []usagecalc.Reading {
	var out []usagecalc.Reading
	for _, r := range rs {
		if !r.At.After(now) {
			out = append(out, r)
		}
	}
	return out
}

func TestFixtureShape(t *testing.T) {
	f := loadFixture(t)
	if len(f.Readings) != 16 {
		t.Fatalf("readings = %d, want 16", len(f.Readings))
	}
	if f.Coefficients != DefaultCoefficients {
		t.Fatalf("fixture coefficients %+v, default %+v", f.Coefficients, DefaultCoefficients)
	}
	if got := f.Recorded["limit_hit_at"]; got != "2026-10-31T05:30:00-04:00" {
		t.Fatalf("limit_hit_at = %v", got)
	}
}

func TestFridayNightProjectsOverWellBeforeLimit(t *testing.T) {
	f := loadFixture(t)
	loc := ny(t)
	limitHit := time.Date(2026, 10, 31, 5, 30, 0, 0, loc)

	var first time.Time
	for now := time.Date(2026, 10, 29, 10, 0, 0, 0, loc); now.Before(limitHit); now = now.Add(time.Hour) {
		p := Project(ProjectInput{Readings: readingsUpTo(f.Readings, now), Reset: f.Reset, Now: now,
			Buckets: f.Buckets, Coef: f.Coefficients})
		if p.Needs != "" {
			t.Fatalf("at %v needs %q", now, p.Needs)
		}
		if p.Over && first.IsZero() {
			first = now
		}
	}
	if first.IsZero() {
		t.Fatal("never projected over")
	}
	if lead := limitHit.Sub(first); lead < 12*time.Hour {
		t.Fatalf("first over at %v, only %v before the limit", first, lead)
	}

	// Friday 17:00: three and a half hours after the 70 percent reading.
	now := time.Date(2026, 10, 30, 17, 0, 0, 0, loc)
	p := Project(ProjectInput{Readings: readingsUpTo(f.Readings, now), Reset: f.Reset, Now: now,
		Buckets: f.Buckets, Coef: f.Coefficients})
	if !p.Over || p.OverBy <= 0 || p.Projected < 120 {
		t.Fatalf("Friday 17:00: over=%v by %.1f projected %.1f", p.Over, p.OverBy, p.Projected)
	}
	if p.PointsPerHour < 1.6 || p.PointsPerHour > 1.9 {
		t.Fatalf("points/hour = %.2f, want about 1.7", p.PointsPerHour)
	}
	if math.Abs(p.AllowedPerHour-0.6) > 0.02 {
		t.Fatalf("allowed/hour = %.3f, want about 0.6", p.AllowedPerHour)
	}
	// At 17:00 the percentage is still on the pace line; the burn rate is what gives it away.
	// By 02:00 it is well ahead.
	late := time.Date(2026, 10, 31, 2, 0, 0, 0, loc)
	q := Project(ProjectInput{Readings: readingsUpTo(f.Readings, late), Reset: f.Reset, Now: late,
		Buckets: f.Buckets, Coef: f.Coefficients})
	if !q.AheadOfPace || q.EstimatedNow < q.LineNow+5 {
		t.Fatalf("Saturday 02:00: estimated %.1f against line %.1f", q.EstimatedNow, q.LineNow)
	}
	if p.ExhaustAt == nil || p.ExhaustAt.After(limitHit.Add(2*time.Hour)) || p.ExhaustAt.Before(limitHit.Add(-4*time.Hour)) {
		t.Fatalf("exhaust at %v, want near %v", p.ExhaustAt, limitHit)
	}
	if p.HoursEarly < 20 {
		t.Fatalf("hours early = %.1f", p.HoursEarly)
	}
	if p.Line <= 0 || p.Ceiling != 100 || p.Target != 95 {
		t.Fatalf("line %.1f ceiling %.1f target %.1f", p.Line, p.Ceiling, p.Target)
	}
	// PointsPerHour is the calibrated blend, so it need not equal the window split; the split
	// itself is still the trailing-window burn of worker and other sessions.
	if p.PointsPerHour <= 0 || p.WorkerPointsPerHour <= p.OtherPointsPerHour {
		t.Fatalf("split worker %.2f other %.2f, points/hour %.2f", p.WorkerPointsPerHour, p.OtherPointsPerHour, p.PointsPerHour)
	}
}

// bucketsAt builds hourly buckets that burn pph points an hour from start for n hours, with
// 100 cache-read tokens per cache-write token.
func bucketsAt(start time.Time, n int, pph float64, c Coefficients) []Bucket {
	cw := pph * 1e6 / (c.CacheWritePerM + c.CacheReadPerM*100)
	var out []Bucket
	for i := 0; i < n; i++ {
		out = append(out, Bucket{Hour: start.Add(time.Duration(i) * time.Hour).UTC().Truncate(time.Hour),
			Other: Tokens{CacheWrite: int64(cw), CacheRead: int64(cw * 100)}})
	}
	return out
}

func TestUnderBudgetStaysUnder(t *testing.T) {
	loc := ny(t)
	reset := usagecalc.ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "America/New_York"}
	pct := 30.0
	readAt := time.Date(2026, 10, 28, 8, 0, 0, 0, loc)
	rs := []usagecalc.Reading{{ID: "a", At: readAt, Percent: &pct, Source: "t"}}
	now := readAt.Add(4 * time.Hour)
	b := bucketsAt(readAt.Add(-6*time.Hour), 10, 0.4, DefaultCoefficients)
	p := Project(ProjectInput{Readings: rs, Reset: reset, Now: now, Buckets: b, Coef: DefaultCoefficients})
	if p.Needs != "" {
		t.Fatalf("needs %q", p.Needs)
	}
	if p.Over || p.OverBy >= 0 {
		t.Fatalf("over=%v by %.1f (projected %.1f target %.1f)", p.Over, p.OverBy, p.Projected, p.Target)
	}
	if p.ExhaustAt != nil || p.HoursEarly != 0 {
		t.Fatalf("exhaust %v early %.1f", p.ExhaustAt, p.HoursEarly)
	}
	if math.Abs(p.PointsPerHour-0.4) > 0.01 {
		t.Fatalf("points/hour = %.3f", p.PointsPerHour)
	}
	// A tight margin the projection does not meet flips the verdict.
	m := 60.0
	q := Project(ProjectInput{Readings: rs, Reset: reset, Now: now, Buckets: b, Coef: DefaultCoefficients, MarginPoints: &m})
	if !q.Over {
		t.Fatalf("margin 60: projected %.1f target %.1f should be over", q.Projected, q.Target)
	}
}

func TestProjectNeeds(t *testing.T) {
	reset := usagecalc.ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "America/New_York"}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if p := Project(ProjectInput{Reset: reset, Now: now}); p.Needs != usagecalc.NeedsReading {
		t.Fatalf("no readings: needs %q", p.Needs)
	}
	pct := 40.0
	old := []usagecalc.Reading{{ID: "a", At: now.Add(-10 * 24 * time.Hour), Percent: &pct}}
	if p := Project(ProjectInput{Readings: old, Reset: reset, Now: now}); p.Needs == "" {
		t.Fatal("reading from before the last reset should need a new reading")
	}
	if p := Project(ProjectInput{Readings: old, Now: now}); p.Needs != usagecalc.NeedsReset {
		t.Fatalf("no reset: needs %q", p.Needs)
	}
}

func TestRefitRecoversCoefficients(t *testing.T) {
	f := loadFixture(t)
	wrong := Coefficients{CacheWritePerM: 0.2, CacheReadPerM: 0.02}
	res := Refit(f.Buckets, f.Readings, wrong, f.Through)
	if !res.Refit {
		t.Fatalf("not refitted: %s", res.Reason)
	}
	if res.Intervals != 15 {
		t.Fatalf("intervals = %d, want 15", res.Intervals)
	}
	if math.Abs(res.Coef.CacheWritePerM-0.373) > 0.02 || math.Abs(res.Coef.CacheReadPerM-0.0069) > 0.0015 {
		t.Fatalf("coef = %+v, want 0.373 and 0.0069", res.Coef)
	}
	if res.RMSE > 0.3 || res.RMSE >= res.PriorRMSE {
		t.Fatalf("rmse %.3f, prior %.3f", res.RMSE, res.PriorRMSE)
	}
	if res.MaxAbsErr < res.RMSE {
		t.Fatalf("max abs %.3f below rmse %.3f", res.MaxAbsErr, res.RMSE)
	}
}

func TestFitterRefitsOnNewReadingOnly(t *testing.T) {
	f := loadFixture(t)
	fit := NewFitter(Coefficients{CacheWritePerM: 0.2, CacheReadPerM: 0.02})
	early := f.Readings[:12]
	if _, did := fit.Observe(f.Buckets, early, f.Through); !did {
		t.Fatal("first reading set should refit")
	}
	if _, did := fit.Observe(f.Buckets, early, f.Through); did {
		t.Fatal("same newest reading refitted again")
	}
	res, did := fit.Observe(f.Buckets, f.Readings, f.Through)
	if !did || !res.Refit {
		t.Fatalf("new reading: did=%v refit=%v %s", did, res.Refit, res.Reason)
	}
	c := fit.Coefficients()
	if math.Abs(c.CacheWritePerM-0.373) > 0.02 {
		t.Fatalf("coef = %+v", c)
	}
	if fit.Last().Intervals != 15 {
		t.Fatalf("last intervals = %d", fit.Last().Intervals)
	}
}

func TestRefitKeepsPriorWhenDataIsThin(t *testing.T) {
	f := loadFixture(t)
	res := Refit(f.Buckets, f.Readings[:3], DefaultCoefficients, f.Through)
	if res.Refit || res.Coef != DefaultCoefficients || res.Reason == "" {
		t.Fatalf("thin data: %+v", res)
	}
	// Cache reads exactly proportional to cache writes cannot be separated.
	var bs []Bucket
	var rs []usagecalc.Reading
	base := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	p := 0.0
	for i := 0; i < 8; i++ {
		bs = append(bs, Bucket{Hour: base.Add(time.Duration(i) * time.Hour), Other: Tokens{CacheWrite: 1e6, CacheRead: 1e8}})
		v := p
		rs = append(rs, usagecalc.Reading{At: base.Add(time.Duration(i) * time.Hour), Percent: &v})
		p += 1.0
	}
	res = Refit(bs, rs, DefaultCoefficients, time.Time{})
	if res.Refit || res.Reason == "" {
		t.Fatalf("collinear data: %+v", res)
	}
}

func TestIntervalsSkipResetAndShortGaps(t *testing.T) {
	base := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	pc := func(v float64) *float64 { return &v }
	rs := []usagecalc.Reading{
		{At: base, Percent: pc(90)},
		{At: base.Add(2 * time.Hour), Percent: pc(95)},
		{At: base.Add(2*time.Hour + 5*time.Minute), Percent: pc(96)},
		{At: base.Add(4 * time.Hour), Percent: pc(3)}, // reset
		{At: base.Add(6 * time.Hour), Percent: pc(8)},
		{At: base.Add(7 * time.Hour), SpendUSD: pc(4)}, // no percent
	}
	ivs := Intervals(nil, rs, time.Time{})
	if len(ivs) != 2 || ivs[0].Points != 5 || ivs[1].Points != 5 {
		t.Fatalf("intervals = %+v", ivs)
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "burnguard", "transcripts")
}

func TestReadDirDedupesAndSplits(t *testing.T) {
	u, err := ReadDir(fixtureDir(t), ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if u.Files != 2 {
		t.Fatalf("files = %d", u.Files)
	}
	// Turns: req_w1, req_w2, msg_only (worker); req_o1, req_o2 (other). req_w1 repeats within
	// the worker file and again in the supervisor file; msg_only repeats once.
	if u.Turns != 5 || u.Duplicates != 3 {
		t.Fatalf("turns %d duplicates %d", u.Turns, u.Duplicates)
	}
	if u.Malformed != 3 { // not json, bad timestamp, truncated line
		t.Fatalf("malformed = %d", u.Malformed)
	}
	if len(u.Buckets) != 3 {
		t.Fatalf("buckets = %+v", u.Buckets)
	}
	h17, h18, h19 := u.Buckets[0], u.Buckets[1], u.Buckets[2]
	if h17.Hour != time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC) || h18.Hour.Hour() != 18 || h19.Hour.Hour() != 19 {
		t.Fatalf("hours = %v %v %v", h17.Hour, h18.Hour, h19.Hour)
	}
	if h17.Worker != (Tokens{Input: 10, Output: 200, CacheWrite: 1000000, CacheRead: 50000000}) {
		t.Fatalf("17h worker = %+v", h17.Worker)
	}
	if h17.Other != (Tokens{Input: 20, Output: 400, CacheWrite: 2000000, CacheRead: 100000000}) {
		t.Fatalf("17h other = %+v", h17.Other)
	}
	if h18.Worker.CacheWrite != 500003 || h18.Worker.CacheRead != 20000004 || h18.Other != (Tokens{}) {
		t.Fatalf("18h = %+v", h18)
	}
	if h19.Worker != (Tokens{}) || h19.Other.CacheWrite != 100000 {
		t.Fatalf("19h = %+v", h19)
	}
	if want := time.Date(2026, 10, 30, 19, 0, 0, 0, time.UTC); !u.Through.Equal(want) {
		t.Fatalf("through = %v", u.Through)
	}
}

func TestReadDirSinceAndErrors(t *testing.T) {
	u, err := ReadDir(fixtureDir(t), ReadOptions{Since: time.Date(2026, 10, 30, 18, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Buckets) != 1 || u.Buckets[0].Hour.Hour() != 19 {
		t.Fatalf("since filter: %+v", u.Buckets)
	}
	if _, err := ReadDir("", ReadOptions{}); err == nil {
		t.Fatal("empty dir should error")
	}
	if _, err := ReadDir(filepath.Join(t.TempDir(), "missing"), ReadOptions{}); err == nil {
		t.Fatal("missing dir should error")
	}
	if u, err := ReadDir(t.TempDir(), ReadOptions{}); err != nil || len(u.Buckets) != 0 {
		t.Fatalf("empty dir: %v %+v", err, u)
	}
}

func TestReadDirCustomMarkerAndLongLine(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-x-workers-y")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	long := make([]byte, 3<<20)
	for i := range long {
		long[i] = 'a'
	}
	body := `{"type":"user","message":{"content":"` + string(long) + `"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-30T10:00:00Z","requestId":"r","message":{"usage":{"cache_creation_input_tokens":7}}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := ReadDir(dir, ReadOptions{WorkerMarker: "workers"})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Buckets) != 1 || u.Buckets[0].Worker.CacheWrite != 7 {
		t.Fatalf("buckets = %+v", u.Buckets)
	}
}
