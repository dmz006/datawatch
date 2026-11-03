package costreport

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeSource is the test Source: it records the query it got and answers
// with a fixed result, so the tests exercise the report against the
// contract, not against a real spend store.
type fakeSource struct {
	result *SourceResult
	err    error
	got    SourceOptions
}

func (f *fakeSource) Records(opts SourceOptions) (*SourceResult, error) {
	f.got = opts
	return f.result, f.err
}

// testRates is the per-backend rate table the tests price with; "ollama"
// deliberately has no entry so the rate-missing paths are covered.
var testRateMap = map[string]session.CostRate{
	"claude-code": {InPerK: 0.01, OutPerK: 0.02},
	"aider":       {InPerK: 0.03, OutPerK: 0.006},
}

func testRate(backend string) (session.CostRate, bool) {
	rate, ok := testRateMap[backend]
	return rate, ok
}

const tokFuzz = 1e-9

var (
	day      = 24 * time.Hour
	fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
)

func fixedDay(month time.Month, day, hour int) time.Time {
	return time.Date(2026, month, day, hour, 0, 0, 0, time.UTC)
}

func makeRecord(sessionID, automationID, automatonID, backend string, at time.Time, in, out int, peer bool) Record {
	return Record{
		SessionID:    sessionID,
		AutomationID: automationID,
		AutomatonID:  automatonID,
		Backend:      backend,
		At:           at,
		TokensIn:     in,
		TokensOut:    out,
		Peer:         peer,
	}
}

func floatEq(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tokFuzz {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func wantRow(t *testing.T, report *Report, key string, want Row) {
	t.Helper()
	for _, row := range report.Rows {
		if row.Key != key {
			continue
		}
		if row.Sessions != want.Sessions || row.TokensIn != want.TokensIn || row.TokensOut != want.TokensOut ||
			row.RateConfigured != want.RateConfigured {
			t.Fatalf("row %q: want %+v, got %+v", key, want, row)
		}
		if want.USDEstimate == nil {
			if row.USDEstimate != nil {
				t.Fatalf("row %q: want no usd_estimate, got %v", key, *row.USDEstimate)
			}
			return
		}
		if row.USDEstimate == nil {
			t.Fatalf("row %q: want usd_estimate %v, got none", key, *want.USDEstimate)
		}
		floatEq(t, *row.USDEstimate, *want.USDEstimate)
		return
	}
	t.Fatalf("row %q not found in %d rows", key, len(report.Rows))
}

func usd(v float64) *float64 { return &v }

func TestCostReportRowsPerAutomaton(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "auto-1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 1000, 2000, false),
		makeRecord("s2", "auto-2", "auto1", "claude-code", fixedDay(time.October, 6, 11), 0, 1000, false),
		makeRecord("s3", "auto-3", "auto2", "aider", fixedDay(time.October, 7, 9), 500, 500, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "auto1", Row{Sessions: 2, TokensIn: 1000, TokensOut: 3000, RateConfigured: true, USDEstimate: usd(0.07)})
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 500, TokensOut: 500, RateConfigured: true, USDEstimate: usd(0.018)})
}

func TestCostReportRowsPerAutomation(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 200, false),
		makeRecord("s2", "a1", "auto1", "aider", fixedDay(time.October, 6, 11), 10, 20, false),
		makeRecord("s3", "a2", "auto2", "aider", fixedDay(time.October, 7, 9), 30, 40, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomation}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "a1", Row{Sessions: 2, TokensIn: 110, TokensOut: 220, RateConfigured: true,
		USDEstimate: usd(0.00542)})
	wantRow(t, rep, "a2", Row{Sessions: 1, TokensIn: 30, TokensOut: 40, RateConfigured: true, USDEstimate: usd(0.00114)})
}

func TestCostReportRowsPerBackend(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 1000, 2000, false),
		makeRecord("s2", "a2", "auto2", "aider", fixedDay(time.October, 7, 9), 500, 500, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByBackend}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "aider", Row{Sessions: 1, TokensIn: 500, TokensOut: 500, RateConfigured: true, USDEstimate: usd(0.018)})
	wantRow(t, rep, "claude-code", Row{Sessions: 1, TokensIn: 1000, TokensOut: 2000, RateConfigured: true, USDEstimate: usd(0.05)})
}

func TestCostReportRowsPerWeek(t *testing.T) {
	records := []Record{
		// Tuesday Oct 6 → monday week 2026-10-05, sunday week 2026-10-04
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 100, false),
		// Sunday Oct 4 → same key under both starts: 2026-10-04
		makeRecord("s2", "a2", "auto2", "aider", fixedDay(time.October, 4, 9), 50, 50, false),
		// Thursday Oct 8 → monday week 2026-10-05, sunday week 2026-10-04
		makeRecord("s3", "a3", "auto3", "claude-code", fixedDay(time.October, 8, 9), 10, 10, false),
	}
	for _, tc := range []struct {
		name string
		cfg  config.CostReportConfig
		want map[string]int // week key → sessions
	}{
		{
			name: "monday",
			cfg:  config.CostReportConfig{WeekStart: ptr("monday")},
			want: map[string]int{"2026-10-04": 1, "2026-10-05": 2},
		},
		{
			name: "sunday",
			cfg:  config.CostReportConfig{WeekStart: ptr("sunday")},
			want: map[string]int{"2026-10-04": 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{result: &SourceResult{Records: records}}
			rep, err := Build(tc.cfg, Request{GroupBy: GroupByWeek}, src, testRate, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Rows) != len(tc.want) {
				t.Fatalf("want %d rows, got %d: %+v", len(tc.want), len(rep.Rows), rep.Rows)
			}
			for key, sessions := range tc.want {
				for _, row := range rep.Rows {
					if row.Key == key && row.Sessions != sessions {
						t.Fatalf("week %s: want %d sessions, got %d", key, sessions, row.Sessions)
					}
				}
			}
		})
	}
}

func TestCostReportUSDEstimateWhenRateConfigured(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// 1000/1K * 0.03 + 1000/1K * 0.006 = 0.036
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 1000, TokensOut: 1000, RateConfigured: true, USDEstimate: usd(0.036)})
}

func TestCostReportUSDEstimateAbsentWhenRateMissing(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		// Group "mixed" spans a priced and an unpriced backend → no estimate.
		makeRecord("s1", "a1", "mixed", "claude-code", fixedDay(time.October, 6, 10), 1000, 0, false),
		makeRecord("s2", "a1", "mixed", "ollama", fixedDay(time.October, 6, 11), 1000, 0, false),
		// Group "local" is entirely unpriced → no estimate either.
		makeRecord("s3", "a2", "local", "ollama", fixedDay(time.October, 6, 12), 42, 42, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// Tokens still come first and stay complete even without a rate.
	wantRow(t, rep, "mixed", Row{Sessions: 2, TokensIn: 2000, TokensOut: 0, RateConfigured: false})
	wantRow(t, rep, "local", Row{Sessions: 1, TokensIn: 42, TokensOut: 42, RateConfigured: false})
}

func TestCostReportUSDEstimateAbsentWhenNoRateSource(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 100, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 100, TokensOut: 100, RateConfigured: false})
}

func TestCostReportWindowBounds(t *testing.T) {
	since := fixedDay(time.October, 5, 0)
	until := fixedDay(time.October, 6, 0)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("before", "a", "auto1", "aider", fixedDay(time.October, 4, 23), 1, 1, false),
		makeRecord("edge-in", "b", "auto1", "aider", since, 10, 10, false),
		makeRecord("mid", "c", "auto1", "aider", fixedDay(time.October, 5, 12), 20, 20, false),
		makeRecord("edge-out", "d", "auto2", "aider", until, 40, 40, false),
		makeRecord("after", "e", "auto2", "aider", fixedDay(time.October, 6, 1), 80, 80, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton, Since: since, Until: until}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !src.got.Since.Equal(since) || !src.got.Until.Equal(until) {
		t.Fatalf("source got window %v..%v, want %v..%v", src.got.Since, src.got.Until, since, until)
	}
	// auto1 keeps edge-in + mid, drops "before"; auto2 keeps only the
	// until-edge session, drops "after".
	wantRow(t, rep, "auto1", Row{Sessions: 2, TokensIn: 30, TokensOut: 30, RateConfigured: true, USDEstimate: usd(0.00108)})
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 40, TokensOut: 40, RateConfigured: true, USDEstimate: usd(0.00144)})
}

func TestCostReportDefaultWindowFromConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		days      *int
		wantSince time.Time
	}{
		{"default seven days", nil, fixedNow.Add(-7 * day)},
		{"configured thirty days", ptr(30), fixedNow.Add(-30 * day)},
		{"nonpositive value stays default", ptr(0), fixedNow.Add(-7 * day)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{result: &SourceResult{}}
			rep, err := Build(config.CostReportConfig{DefaultWindowDays: tc.days}, Request{Until: fixedNow}, src, testRate, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if !rep.Until.Equal(fixedNow) {
				t.Fatalf("until: want %v, got %v", fixedNow, rep.Until)
			}
			if !rep.Since.Equal(tc.wantSince) {
				t.Fatalf("since: want %v, got %v", tc.wantSince, rep.Since)
			}
			if !src.got.Since.Equal(tc.wantSince) || !src.got.Until.Equal(fixedNow) {
				t.Fatalf("source got %v..%v, want %v..%v", src.got.Since, src.got.Until, tc.wantSince, fixedNow)
			}
		})
	}
}

func TestCostReportRetentionAndFallbackFields(t *testing.T) {
	src := &fakeSource{result: &SourceResult{
		Records:          []Record{makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1, 1, false)},
		LifetimeFallback: true,
	}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RetentionDays != 31 {
		t.Fatalf("retention_days: want 31, got %d", rep.RetentionDays)
	}
	if !rep.LifetimeFallback {
		t.Fatal("lifetime_fallback: want true, got false")
	}
	// And it stays off when the source did not fall back.
	src2 := &fakeSource{result: &SourceResult{}}
	rep2, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src2, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.RetentionDays != RetentionDays {
		t.Fatalf("retention_days: want %d, got %d", RetentionDays, rep2.RetentionDays)
	}
	if rep2.LifetimeFallback {
		t.Fatal("lifetime_fallback: want false, got true")
	}
}

func TestCostReportConfigDefaults(t *testing.T) {
	var cfg config.CostReportConfig
	if got := cfg.EffectiveGroupBy(); got != "automaton" {
		t.Fatalf("default_group_by: want automaton, got %q", got)
	}
	if got := cfg.EffectiveWindowDays(); got != config.DefaultCostReportWindowDays && got != 7 {
		t.Fatalf("default_window_days: want 7, got %d", got)
	}
	if got := cfg.EffectiveShowUSDEstimates(); !got {
		t.Fatal("show_usd_estimates: want default true")
	}
	if got := cfg.EffectiveWeekStart(); got != config.DefaultCostReportWeekStart && got != "monday" {
		t.Fatalf("week_start: want monday, got %q", got)
	}
	if got := cfg.EffectiveIncludePeerSpend(); got {
		t.Fatal("include_peer_spend: want default false")
	}
}

func TestCostReportGroupByDefaultsTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  string
		want GroupBy
	}{
		{"absent", "", GroupByAutomaton},
		{"automaton", "automaton", GroupByAutomaton},
		{"automation", "automation", GroupByAutomation},
		{"backend", "backend", GroupByBackend},
		{"week", "week", GroupByWeek},
		{"unknown falls back to automaton", "bogus", GroupByAutomaton},
		{"case-insensitive", "Week", GroupByWeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.CostReportConfig
			if tc.cfg != "" {
				cfg.DefaultGroupBy = &tc.cfg
			}
			src := &fakeSource{result: &SourceResult{}}
			rep, err := Build(cfg, Request{}, src, testRate, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if rep.GroupBy != tc.want {
				t.Fatalf("group_by: want %q, got %q", tc.want, rep.GroupBy)
			}
		})
	}
}

func TestCostReportInvalidGroupByIsRejected(t *testing.T) {
	src := &fakeSource{result: &SourceResult{}}
	if _, err := Build(config.CostReportConfig{}, Request{GroupBy: "everything"}, src, testRate, fixedNow); err == nil {
		t.Fatal("want an error for an unknown group_by, got none")
	}
}

func TestCostReportPeerSpendExcludedByDefault(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("own", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 10, 10, false),
		makeRecord("peer", "a2", "auto2", "aider", fixedDay(time.October, 6, 11), 99, 99, true),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if src.got.IncludePeerSpend {
		t.Fatal("source options: include_peer_spend should default to false")
	}
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 10, TokensOut: 10, RateConfigured: true, USDEstimate: usd(0.00036)})
	for _, row := range rep.Rows {
		if row.Key == "auto2" {
			t.Fatalf("peer row should be excluded, got %+v", row)
		}
	}
}

func TestCostReportPeerSpendIncludedWhenConfigured(t *testing.T) {
	cfg := config.CostReportConfig{IncludePeerSpend: ptr(true)}
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("own", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 10, 10, false),
		makeRecord("peer", "a2", "auto2", "aider", fixedDay(time.October, 6, 11), 99, 99, true),
	}}}
	rep, err := Build(cfg, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !src.got.IncludePeerSpend {
		t.Fatal("source options: include_peer_spend should be true when configured")
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want both rows, got %+v", rep.Rows)
	}
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 99, TokensOut: 99, RateConfigured: true, USDEstimate: usd(0.003564)})
}

func TestCostReportUSDEstimatesHiddenWhenConfiguredOff(t *testing.T) {
	cfg := config.CostReportConfig{ShowUSDEstimates: ptr(false)}
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(cfg, Request{GroupBy: GroupByAutomaton}, src, testRate, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// Tokens still show; the estimate is absent and the row stays unpriced.
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 1000, TokensOut: 1000, RateConfigured: false})
}

func TestCostReportSourceFailureSurfaces(t *testing.T) {
	wanted := errors.New("store unavailable")
	src := &fakeSource{err: wanted}
	if _, err := Build(config.CostReportConfig{}, Request{}, src, testRate, fixedNow); err == nil || !errors.Is(err, wanted) {
		t.Fatalf("want wrapped source error, got %v", err)
	}
}

func TestCostReportNilSourceIsRejected(t *testing.T) {
	if _, err := Build(config.CostReportConfig{}, Request{}, nil, testRate, fixedNow); err == nil {
		t.Fatal("want an error for a nil source, got none")
	}
}

func ptr[T any](v T) *T { return &v }
