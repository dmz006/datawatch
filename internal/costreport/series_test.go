package costreport

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/spendledger"
)

func ledgerEntry(session, automaton, backend string, at time.Time, in, out int64) spendledger.Entry {
	return spendledger.Entry{
		SessionID: session, AutomatonID: automaton, Role: spendledger.RoleWorker,
		Backend: backend, StartedAt: at, TokensIn: in, TokensOut: out, ActiveSeconds: 10,
	}
}

func seriesOpts() SeriesOptions {
	return SeriesOptions{
		Now:       fixedNow,
		RateFor:   testRate,
		Billing:   unitBilling(unitEntries),
		ProjectOf: func(id string) string { return map[string]string{"a1": "alpha", "a2": "alpha", "b1": "beta"}[id] },
	}
}

func groupByKey(t *testing.T, s *Series, key string) *SeriesGroup {
	t.Helper()
	for i := range s.Groups {
		if s.Groups[i].Key == key {
			return &s.Groups[i]
		}
	}
	t.Fatalf("group %q missing from %+v", key, s.Groups)
	return nil
}

func unitIn(units []UnitTotal, unit string) *UnitTotal {
	for i := range units {
		if units[i].Unit == unit {
			return &units[i]
		}
	}
	return nil
}

func TestSeriesHourBucketsForShortWindow(t *testing.T) {
	// fixedNow is 12:00 on Oct 8; a 6h window reaches back to 06:00.
	entries := []spendledger.Entry{
		ledgerEntry("s1", "a1", "aider", fixedDay(time.October, 8, 7), 100, 50),
		ledgerEntry("s2", "a1", "aider", time.Date(2026, 10, 8, 7, 45, 0, 0, time.UTC), 10, 5),
		ledgerEntry("s3", "a1", "aider", fixedDay(time.October, 8, 10), 20, 20),
	}
	s, err := BuildSeries(SeriesRequest{GroupBy: SeriesByAutomaton, Window: 6 * time.Hour}, entries, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Bucket != SeriesBucketHour {
		t.Fatalf("bucket = %q, want hour for a 6h window", s.Bucket)
	}
	if len(s.Buckets) != 7 || !s.Buckets[0].Equal(fixedDay(time.October, 8, 6)) || !s.Buckets[6].Equal(fixedDay(time.October, 8, 12)) {
		t.Fatalf("buckets wrong: %v", s.Buckets)
	}
	g := groupByKey(t, s, "a1")
	if len(g.Points) != len(s.Buckets) {
		t.Fatalf("points %d != buckets %d", len(g.Points), len(s.Buckets))
	}
	if p := g.Points[1]; p.Sessions != 2 || p.TokensIn != 110 || p.TokensOut != 55 || p.Tokens != 165 {
		t.Fatalf("07:00 bucket wrong: %+v", p)
	}
	if p := g.Points[0]; p.Tokens != 0 || p.Sessions != 0 || len(p.Units) != 0 || !p.Start.Equal(s.Buckets[0]) {
		t.Fatalf("empty bucket should be zero-filled: %+v", p)
	}
	if p := g.Points[4]; p.Tokens != 40 {
		t.Fatalf("10:00 bucket wrong: %+v", p)
	}
	if g.Sessions != 3 || g.Tokens != 205 || g.TokensIn != 130 || g.TokensOut != 75 {
		t.Fatalf("group totals wrong: %+v", g)
	}
}

func TestSeriesDayBucketsForWideWindow(t *testing.T) {
	entries := []spendledger.Entry{
		ledgerEntry("s1", "a1", "aider", time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), 10, 10),
		ledgerEntry("s2", "a1", "aider", time.Date(2026, 10, 5, 23, 59, 0, 0, time.UTC), 5, 5),
		ledgerEntry("s3", "a1", "aider", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), 1, 1),
	}
	s, err := BuildSeries(SeriesRequest{Window: 72 * time.Hour}, entries, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Bucket != SeriesBucketDay || s.GroupBy != SeriesByAutomaton {
		t.Fatalf("defaults wrong: bucket=%q group_by=%q", s.Bucket, s.GroupBy)
	}
	// 72h before Oct 8 12:00 is Oct 5 12:00; the first bucket floors to Oct 5.
	if len(s.Buckets) != 4 || !s.Buckets[0].Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("buckets wrong: %v", s.Buckets)
	}
	g := groupByKey(t, s, "a1")
	// s1 (Oct 5 01:00) is before the window start (Oct 5 12:00) and is dropped.
	if g.Points[0].Tokens != 10 || g.Points[0].Sessions != 1 {
		t.Fatalf("Oct 5 bucket wrong: %+v", g.Points[0])
	}
	if g.Points[2].Tokens != 2 || g.Points[1].Tokens != 0 {
		t.Fatalf("later buckets wrong: %+v", g.Points)
	}
}

func TestSeriesExplicitBucketOverridesAuto(t *testing.T) {
	s, err := BuildSeries(SeriesRequest{Window: 7 * 24 * time.Hour, Bucket: SeriesBucketHour}, nil, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Bucket != SeriesBucketHour || len(s.Buckets) != 7*24+1 {
		t.Fatalf("bucket=%q n=%d", s.Bucket, len(s.Buckets))
	}
	s, err = BuildSeries(SeriesRequest{Window: 6 * time.Hour, Bucket: SeriesBucketDay}, nil, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Bucket != SeriesBucketDay {
		t.Fatalf("explicit day lost: %q", s.Bucket)
	}
}

func TestSeriesGroupByAutomatonProjectModel(t *testing.T) {
	at := fixedDay(time.October, 7, 9)
	entries := []spendledger.Entry{
		ledgerEntry("s1", "a1", "aider", at, 100, 0),
		ledgerEntry("s2", "a2", "claude-code", at, 200, 0),
		ledgerEntry("s3", "b1", "aider", at, 400, 0),
		ledgerEntry("s4", "", "ollama", at, 800, 0),
		ledgerEntry("s5", "gone", "", at, 1600, 0),
	}
	req := func(g SeriesGroupBy) *Series {
		s, err := BuildSeries(SeriesRequest{GroupBy: g, Window: 7 * 24 * time.Hour}, entries, seriesOpts())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	byAuto := req(SeriesByAutomaton)
	if got := groupByKey(t, byAuto, "a1").Tokens; got != 100 {
		t.Fatalf("a1 = %d", got)
	}
	if got := groupByKey(t, byAuto, UnattributedKey).Tokens; got != 800 {
		t.Fatalf("unattributed automaton = %d", got)
	}
	if len(byAuto.Groups) != 5 {
		t.Fatalf("want 5 automaton groups, got %d", len(byAuto.Groups))
	}

	byProject := req(SeriesByProject)
	if got := groupByKey(t, byProject, "alpha").Tokens; got != 300 {
		t.Fatalf("alpha = %d, want a1+a2", got)
	}
	if got := groupByKey(t, byProject, "beta").Tokens; got != 400 {
		t.Fatalf("beta = %d", got)
	}
	// no automaton, and an automaton with no known project, both unattributed
	if got := groupByKey(t, byProject, UnattributedKey).Tokens; got != 800+1600 {
		t.Fatalf("unattributed project = %d", got)
	}

	byModel := req(SeriesByModel)
	if got := groupByKey(t, byModel, "aider").Tokens; got != 500 {
		t.Fatalf("aider = %d", got)
	}
	if got := groupByKey(t, byModel, "claude-code").Tokens; got != 200 {
		t.Fatalf("claude-code = %d", got)
	}
	if got := groupByKey(t, byModel, UnattributedKey).Tokens; got != 1600 {
		t.Fatalf("blank backend = %d", got)
	}
	// groups are sorted by key
	for i := 1; i < len(byModel.Groups); i++ {
		if byModel.Groups[i-1].Key > byModel.Groups[i].Key {
			t.Fatalf("groups not sorted: %+v", byModel.Groups)
		}
	}
}

func TestSeriesProjectWithoutResolver(t *testing.T) {
	opts := seriesOpts()
	opts.ProjectOf = nil
	s, err := BuildSeries(SeriesRequest{GroupBy: SeriesByProject, Window: 24 * time.Hour},
		[]spendledger.Entry{ledgerEntry("s1", "a1", "aider", fixedDay(time.October, 8, 1), 1, 1)}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Groups) != 1 || s.Groups[0].Key != UnattributedKey {
		t.Fatalf("want one unattributed group, got %+v", s.Groups)
	}
}

func TestSeriesEmptyWindow(t *testing.T) {
	entries := []spendledger.Entry{
		ledgerEntry("old", "a1", "aider", fixedDay(time.September, 1, 1), 5, 5),  // before the window
		ledgerEntry("future", "a1", "aider", fixedDay(time.October, 9, 1), 5, 5), // after now
		{SessionID: "nostamp", AutomatonID: "a1", Role: spendledger.RoleWorker, TokensIn: 9},
	}
	for name, in := range map[string][]spendledger.Entry{"no entries": nil, "all outside": entries} {
		s, err := BuildSeries(SeriesRequest{Window: 24 * time.Hour}, in, seriesOpts())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.Groups == nil || len(s.Groups) != 0 {
			t.Fatalf("%s: groups must be an empty list, got %#v", name, s.Groups)
		}
		if len(s.Buckets) == 0 {
			t.Fatalf("%s: the empty window still has an x axis", name)
		}
		if len(s.TotalsByUnit) != 0 {
			t.Fatalf("%s: no unit totals expected: %+v", name, s.TotalsByUnit)
		}
		if s.RetentionDays != RetentionDays || s.UnknownSpendNote != "" {
			t.Fatalf("%s: retention fields wrong: %+v", name, s)
		}
	}
}

func TestSeriesMixedUnitsStaySeparate(t *testing.T) {
	at := fixedDay(time.October, 8, 9)
	entries := []spendledger.Entry{
		ledgerEntry("s1", "a1", "claude-code", at, 1000, 2000), // bundled plan -> points
		ledgerEntry("s2", "a1", "aider", at, 500, 500),         // api -> dollars
		ledgerEntry("s3", "a1", "ollama", at, 100, 100),        // local -> tokens only
		ledgerEntry("s4", "a1", "aider", fixedDay(time.October, 8, 11), 500, 500),
	}
	s, err := BuildSeries(SeriesRequest{GroupBy: SeriesByAutomaton, Window: 6 * time.Hour}, entries, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	g := groupByKey(t, s, "a1")
	bucket := g.Points[3] // 09:00 when the window starts at 06:00
	if bucket.Tokens != 1000+2000+500+500+100+100 {
		t.Fatalf("tokens are summed across units: %+v", bucket)
	}
	if len(bucket.Units) != 3 {
		t.Fatalf("want 3 units in the 09:00 bucket, got %+v", bucket.Units)
	}
	d := unitIn(bucket.Units, UnitDollars)
	if d == nil || d.Dollars == nil || d.Points != nil {
		t.Fatalf("dollars unit wrong: %+v", d)
	}
	floatEq(t, *d.Dollars, 0.018) // aider only; never plus the plan's estimate
	if d.TokensIn != 500 || d.BillingMode != config.BillingModeAPIMetered {
		t.Fatalf("dollars unit tokens/mode wrong: %+v", d)
	}
	p := unitIn(bucket.Units, UnitPoints)
	if p == nil || p.Points == nil || p.Dollars != nil || p.Percent == nil || p.BillingMode != config.BillingModeBundledPlan {
		t.Fatalf("points unit wrong: %+v", p)
	}
	pc := config.BillingProfile{MonthlyPrice: 100}.PointCostEquivalent()
	floatEq(t, *p.Points, 0.05/pc) // 1000 in + 2000 out at claude-code rates is 0.05 USD-equivalent
	floatEq(t, *p.Percent, 0.05/100*100)
	n := unitIn(bucket.Units, UnitNone)
	if n == nil || n.Dollars != nil || n.Points != nil || n.Percent != nil || n.TokensIn != 100 {
		t.Fatalf("local unit wrong: %+v", n)
	}
	// units come out in a stable order: dollars, points, none
	if bucket.Units[0].Unit != UnitDollars || bucket.Units[1].Unit != UnitPoints || bucket.Units[2].Unit != UnitNone {
		t.Fatalf("unit order wrong: %+v", bucket.Units)
	}

	// the 11:00 bucket only has api spend, so only a dollars unit
	late := g.Points[5]
	if len(late.Units) != 1 || late.Units[0].Unit != UnitDollars {
		t.Fatalf("11:00 bucket units wrong: %+v", late.Units)
	}
	// the group and the series total the units across buckets without mixing them
	gd := unitIn(g.UnitTotals, UnitDollars)
	floatEq(t, *gd.Dollars, 0.036)
	td := unitIn(s.TotalsByUnit, UnitDollars)
	floatEq(t, *td.Dollars, 0.036)
	if len(s.TotalsByUnit) != 3 {
		t.Fatalf("series totals should hold three units: %+v", s.TotalsByUnit)
	}
}

func TestSeriesMoneyOmittedWithoutRateOrBilling(t *testing.T) {
	at := fixedDay(time.October, 8, 9)
	entries := []spendledger.Entry{ledgerEntry("s1", "a1", "aider", at, 500, 500)}

	// No billing resolver: tokens only, no units at all.
	opts := seriesOpts()
	opts.Billing = nil
	s, err := BuildSeries(SeriesRequest{Window: 6 * time.Hour}, entries, opts)
	if err != nil {
		t.Fatal(err)
	}
	if g := groupByKey(t, s, "a1"); g.Tokens != 1000 || g.UnitTotals != nil || len(g.Points[3].Units) != 0 || s.TotalsByUnit != nil {
		t.Fatalf("tokens-only shape wrong: %+v", g)
	}

	// Billing but no rate for the backend: the unit carries tokens, no dollars.
	opts = seriesOpts()
	opts.RateFor = func(string) (session.CostRate, bool) { return session.CostRate{}, false }
	s, err = BuildSeries(SeriesRequest{Window: 6 * time.Hour}, entries, opts)
	if err != nil {
		t.Fatal(err)
	}
	d := unitIn(groupByKey(t, s, "a1").Points[3].Units, UnitDollars)
	if d == nil || d.Dollars != nil || d.TokensIn != 500 {
		t.Fatalf("unrated unit wrong: %+v", d)
	}

	// USD display off: same, even with a rate.
	opts = seriesOpts()
	off := false
	opts.Config.ShowUSDEstimates = &off
	s, err = BuildSeries(SeriesRequest{Window: 6 * time.Hour}, entries, opts)
	if err != nil {
		t.Fatal(err)
	}
	d = unitIn(groupByKey(t, s, "a1").Points[3].Units, UnitDollars)
	if d == nil || d.Dollars != nil {
		t.Fatalf("USD display off should hide dollars: %+v", d)
	}
}

func TestSeriesDefaultWindowFromConfig(t *testing.T) {
	opts := seriesOpts()
	days := 3
	opts.Config.DefaultWindowDays = &days
	s, err := BuildSeries(SeriesRequest{}, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Window != (72*time.Hour).String() || s.Bucket != SeriesBucketDay {
		t.Fatalf("window=%q bucket=%q", s.Window, s.Bucket)
	}
	s, err = BuildSeries(SeriesRequest{}, nil, SeriesOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if s.Window != (time.Duration(config.DefaultCostReportWindowDays) * 24 * time.Hour).String() {
		t.Fatalf("built-in default window wrong: %q", s.Window)
	}
}

func TestSeriesUnknownSpendBeforeLedgerHorizon(t *testing.T) {
	s, err := BuildSeries(SeriesRequest{Window: 40 * 24 * time.Hour}, nil, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.UnknownBefore == nil || !strings.Contains(s.UnknownSpendNote, "unknown") {
		t.Fatalf("a 40-day window should flag unknown spend: %+v", s)
	}
	s, err = BuildSeries(SeriesRequest{Window: 7 * 24 * time.Hour}, nil, seriesOpts())
	if err != nil {
		t.Fatal(err)
	}
	if s.UnknownBefore != nil || s.UnknownSpendNote != "" {
		t.Fatalf("a 7-day window must not flag unknown spend: %+v", s)
	}
}

func TestSeriesRejectsBadRequests(t *testing.T) {
	cases := map[string]SeriesRequest{
		"group_by": {GroupBy: "week"},
		"bucket":   {Bucket: "minute"},
		"negative": {Window: -time.Hour},
		"too many": {Window: 60 * 24 * time.Hour, Bucket: SeriesBucketHour},
	}
	for name, r := range cases {
		if _, err := BuildSeries(r, nil, seriesOpts()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// the clock defaults when Now is zero
	if _, err := BuildSeries(SeriesRequest{Window: time.Hour}, nil, SeriesOptions{}); err != nil {
		t.Fatalf("zero Now: %v", err)
	}
}

func TestParseSeriesWindow(t *testing.T) {
	ok := map[string]time.Duration{
		"24h": 24 * time.Hour, "90m": 90 * time.Minute, "7d": 7 * 24 * time.Hour,
		"2w": 14 * 24 * time.Hour, " 1D ": 24 * time.Hour, "0.5d": 12 * time.Hour,
	}
	for in, want := range ok {
		got, err := ParseSeriesWindow(in)
		if err != nil || got != want {
			t.Errorf("%q -> %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "d", "abc", "-1d", "0h", "xd", "7x"} {
		if _, err := ParseSeriesWindow(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}
