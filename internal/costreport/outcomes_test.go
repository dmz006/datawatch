package costreport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// A fixed Thursday so week edges are predictable: the week starts Monday
// 2026-11-09 00:00 UTC.
var outcomesNow = time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC)

func toolGroup(t *testing.T, tu *ToolUsage, key string) ToolUsageGroup {
	t.Helper()
	for _, g := range tu.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("group %q missing: %+v", key, tu.Groups)
	return ToolUsageGroup{}
}

func toolEntries() []spendledger.Entry {
	now := outcomesNow
	return []spendledger.Entry{
		{SessionID: "s1", AutomatonID: "a1", Backend: "aider", StartedAt: now.Add(-3 * time.Hour), ToolCalls: 12},
		{SessionID: "s2", AutomatonID: "a1", Backend: "aider", StartedAt: now.Add(-2 * time.Hour), ToolCalls: 8},
		{SessionID: "s3", AutomatonID: "a2", Backend: "ollama", StartedAt: now.Add(-90 * time.Minute), ToolCalls: 5},
		{SessionID: "s4", Backend: "ollama", StartedAt: now.Add(-30 * time.Minute), ToolCalls: 1},
		{SessionID: "old", AutomatonID: "a1", Backend: "aider", StartedAt: now.Add(-10 * 24 * time.Hour), ToolCalls: 999},
		{SessionID: "undated", AutomatonID: "a1", Backend: "aider", ToolCalls: 77},
		{SessionID: "future", AutomatonID: "a1", Backend: "aider", StartedAt: now.Add(time.Hour), ToolCalls: 55},
	}
}

func TestBuildToolUsage_ByAutomatonSumsToolCallsPerBucket(t *testing.T) {
	tu, err := BuildToolUsage(SeriesRequest{Window: 24 * time.Hour}, toolEntries(), SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	if tu.GroupBy != SeriesByAutomaton || tu.Bucket != SeriesBucketHour {
		t.Fatalf("group_by=%q bucket=%q", tu.GroupBy, tu.Bucket)
	}
	if len(tu.Groups) != 3 {
		t.Fatalf("want 3 groups (old, undated and future entries are out), got %+v", tu.Groups)
	}
	a1 := toolGroup(t, tu, "a1")
	if a1.ToolCalls != 20 || a1.Sessions != 2 {
		t.Errorf("a1 = %+v", a1)
	}
	if g := toolGroup(t, tu, UnattributedKey); g.ToolCalls != 1 {
		t.Errorf("unattributed = %+v", g)
	}
	if tu.TotalToolCalls != 26 || tu.TotalSessions != 4 {
		t.Errorf("totals = %d calls, %d sessions", tu.TotalToolCalls, tu.TotalSessions)
	}
	if len(a1.Points) != len(tu.Buckets) {
		t.Fatalf("points %d != buckets %d", len(a1.Points), len(tu.Buckets))
	}
	sum := 0
	for i, p := range a1.Points {
		if !p.Start.Equal(tu.Buckets[i]) {
			t.Errorf("point %d start %v != bucket %v", i, p.Start, tu.Buckets[i])
		}
		sum += p.ToolCalls
	}
	if sum != a1.ToolCalls {
		t.Errorf("points sum %d != line total %d", sum, a1.ToolCalls)
	}
	// each session lands in its own hour: 12 in the -3h bucket, 8 in -2h
	byStart := map[time.Time]ToolUsagePoint{}
	for _, p := range a1.Points {
		byStart[p.Start] = p
	}
	if p := byStart[outcomesNow.Add(-3*time.Hour).Truncate(time.Hour)]; p.ToolCalls != 12 || p.Sessions != 1 {
		t.Errorf("-3h bucket = %+v", p)
	}
	if p := byStart[outcomesNow.Add(-2*time.Hour).Truncate(time.Hour)]; p.ToolCalls != 8 {
		t.Errorf("-2h bucket = %+v", p)
	}
}

func TestBuildToolUsage_NoteSaysNoPerToolNames(t *testing.T) {
	tu, err := BuildToolUsage(SeriesRequest{Window: 24 * time.Hour}, nil, SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	if tu.ToolNamesNote != ToolNamesNote || !strings.Contains(tu.ToolNamesNote, "no per-tool-name breakdown") {
		t.Errorf("note = %q", tu.ToolNamesNote)
	}
	raw, _ := json.Marshal(tu)
	s := string(raw)
	if !strings.Contains(s, `"tool_names_note"`) || !strings.Contains(s, `"groups":[]`) {
		t.Errorf("empty payload must carry the note and serialize groups as []: %s", s)
	}
	if strings.Contains(s, `"tools"`) || strings.Contains(s, `"by_tool"`) {
		t.Errorf("payload must not invent a per-tool breakdown: %s", s)
	}
}

func TestBuildToolUsage_GroupByModelAndProject(t *testing.T) {
	entries := toolEntries()
	byModel, err := BuildToolUsage(SeriesRequest{GroupBy: SeriesByModel, Window: 24 * time.Hour}, entries, SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	if g := toolGroup(t, byModel, "aider"); g.ToolCalls != 20 {
		t.Errorf("aider = %+v", g)
	}
	if g := toolGroup(t, byModel, "ollama"); g.ToolCalls != 6 || g.Sessions != 2 {
		t.Errorf("ollama = %+v", g)
	}

	of := func(id string) string {
		if id == "a1" || id == "a2" {
			return "alpha"
		}
		return ""
	}
	byProject, err := BuildToolUsage(SeriesRequest{GroupBy: SeriesByProject, Window: 24 * time.Hour}, entries, SeriesOptions{Now: outcomesNow, ProjectOf: of})
	if err != nil {
		t.Fatal(err)
	}
	if len(byProject.Groups) != 2 || byProject.Groups[0].Key != "alpha" || byProject.Groups[0].ToolCalls != 25 {
		t.Fatalf("project grouping wrong: %+v", byProject.Groups)
	}
}

func TestBuildToolUsage_DayBucketsAndAutoChoice(t *testing.T) {
	tu, err := BuildToolUsage(SeriesRequest{}, toolEntries(), SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	if tu.Bucket != SeriesBucketDay {
		t.Errorf("default window should bucket by day, got %q", tu.Bucket)
	}
	if tu.Window != (7 * 24 * time.Hour).String() {
		t.Errorf("default window = %q", tu.Window)
	}
	if g := toolGroup(t, tu, "a1"); g.ToolCalls != 20 {
		t.Errorf("a1 (the 10-day-old entry is outside 7d) = %+v", g)
	}
	tu, err = BuildToolUsage(SeriesRequest{Window: 24 * time.Hour, Bucket: SeriesBucketDay}, toolEntries(), SeriesOptions{Now: outcomesNow})
	if err != nil || tu.Bucket != SeriesBucketDay || len(tu.Buckets) != 2 {
		t.Fatalf("explicit day bucket: %v %+v", err, tu)
	}
}

func TestBuildToolUsage_ConfigWindowAndNegativeCalls(t *testing.T) {
	days := 2
	cfg := config.CostReportConfig{DefaultWindowDays: &days}
	entries := []spendledger.Entry{
		{SessionID: "a", AutomatonID: "a1", StartedAt: outcomesNow.Add(-time.Hour), ToolCalls: -4},
		{SessionID: "b", AutomatonID: "a1", StartedAt: outcomesNow.Add(-time.Hour), ToolCalls: 3},
	}
	tu, err := BuildToolUsage(SeriesRequest{}, entries, SeriesOptions{Now: outcomesNow, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if tu.Window != (48*time.Hour).String() || tu.Bucket != SeriesBucketHour {
		t.Errorf("window=%q bucket=%q", tu.Window, tu.Bucket)
	}
	if g := toolGroup(t, tu, "a1"); g.ToolCalls != 3 || g.Sessions != 2 {
		t.Errorf("a negative count must read as zero: %+v", g)
	}
}

func TestBuildToolUsage_UnknownBeforeHorizon(t *testing.T) {
	tu, err := BuildToolUsage(SeriesRequest{Window: 60 * 24 * time.Hour}, nil, SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	if tu.UnknownSpendNote == "" || tu.UnknownBefore == nil || tu.RetentionDays != RetentionDays {
		t.Errorf("a window past the ledger horizon must say so: %+v", tu)
	}
	tu, _ = BuildToolUsage(SeriesRequest{Window: 24 * time.Hour}, nil, SeriesOptions{Now: outcomesNow})
	if tu.UnknownSpendNote != "" || tu.UnknownBefore != nil {
		t.Errorf("a window inside the horizon has no unknown note: %+v", tu)
	}
}

func TestBuildToolUsage_Validation(t *testing.T) {
	for name, req := range map[string]SeriesRequest{
		"group_by":        {GroupBy: "week"},
		"bucket":          {Bucket: "minute"},
		"negative window": {Window: -time.Hour},
		"too many points": {Window: 90 * 24 * time.Hour, Bucket: SeriesBucketHour},
	} {
		if _, err := BuildToolUsage(req, nil, SeriesOptions{Now: outcomesNow}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestBuildToolUsage_ZeroNowReadsClock(t *testing.T) {
	tu, err := BuildToolUsage(SeriesRequest{Window: time.Hour}, nil, SeriesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(tu.Until) > time.Minute {
		t.Errorf("until = %v", tu.Until)
	}
}

// ---- failures ----

func failGroup(t *testing.T, f *Failures, key string) FailureGroup {
	t.Helper()
	for _, g := range f.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("group %q missing: %+v", key, f.Groups)
	return FailureGroup{}
}

func catCount(cs []FailureCategoryCount, cat string) int {
	for _, c := range cs {
		if c.Category == cat {
			return c.Attempts
		}
	}
	return 0
}

func failRecords() []FailureRecord {
	thisWeek := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) // Tuesday of the current week
	lastWeek := thisWeek.AddDate(0, 0, -7)
	return []FailureRecord{
		{AutomatonID: "a1", AutomatonTitle: "Alpha work", AutomationID: "n1", AutomationName: "Nightly", Project: "alpha", TaskID: "t1", At: thisWeek, Outcome: "failed", Category: "infra", Summary: "tmux gone", RetryCount: 2},
		{AutomatonID: "a1", AutomatonTitle: "Alpha work", AutomationID: "n1", AutomationName: "Nightly", Project: "alpha", TaskID: "t1", At: thisWeek.Add(time.Hour), Outcome: "failed", Category: "infra", Summary: "tmux  gone\n", RetryCount: 2},
		{AutomatonID: "a1", AutomatonTitle: "Alpha work", AutomationID: "n1", AutomationName: "Nightly", Project: "alpha", TaskID: "t2", At: lastWeek, Outcome: "stalled", Category: "content", Summary: "tests fail", RetryCount: 1},
		{AutomatonID: "a2", AutomatonTitle: "Beta work", Project: "alpha", TaskID: "t1", At: lastWeek, Outcome: "cancelled", Category: "", RetryCount: 0},
		{AutomatonID: "a3", AutomatonTitle: "Gamma work", Project: "", TaskID: "t9", At: thisWeek, Outcome: "failed", Category: "content", Summary: "wrong output", RetryCount: 3},
		{AutomatonID: "a1", TaskID: "t1", At: thisWeek.AddDate(0, -3, 0), Category: "infra"}, // outside a 4-week window
		{AutomatonID: "a1", TaskID: "t1", Category: "infra"}, // undated
	}
}

func TestBuildFailures_ByProjectCountsCategoriesAndWeeks(t *testing.T) {
	f, err := BuildFailures(FailuresRequest{Window: 4 * 7 * 24 * time.Hour}, failRecords(), outcomesNow)
	if err != nil {
		t.Fatal(err)
	}
	if f.GroupBy != FailuresByProject {
		t.Fatalf("group_by = %q", f.GroupBy)
	}
	if f.UndatedAttempts != 1 {
		t.Errorf("undated = %d", f.UndatedAttempts)
	}
	alpha := failGroup(t, f, "alpha")
	if alpha.Attempts != 4 || alpha.Tasks != 3 {
		t.Fatalf("alpha = %+v", alpha)
	}
	if catCount(alpha.Categories, "infra") != 2 || catCount(alpha.Categories, "content") != 1 || catCount(alpha.Categories, UncategorizedFailure) != 1 {
		t.Errorf("alpha categories = %+v", alpha.Categories)
	}
	if alpha.Categories[0].Category != "infra" {
		t.Errorf("categories must be most frequent first: %+v", alpha.Categories)
	}
	// retries are counted once per task: a1/t1 has 2, a1/t2 has 1, a2/t1 has 0
	if alpha.Retries != 3 {
		t.Errorf("alpha retries = %d, want 3", alpha.Retries)
	}
	if g := failGroup(t, f, UnattributedKey); g.Attempts != 1 || g.Retries != 3 {
		t.Errorf("unattributed = %+v", g)
	}
	if f.TotalAttempts != 5 || f.TotalTasks != 4 || f.TotalRetries != 6 {
		t.Errorf("totals attempts=%d tasks=%d retries=%d", f.TotalAttempts, f.TotalTasks, f.TotalRetries)
	}
	if catCount(f.Categories, "infra") != 2 || catCount(f.Categories, "content") != 2 {
		t.Errorf("overall categories = %+v", f.Categories)
	}

	// weeks run Monday to Sunday, zero-filled
	if len(alpha.Weeks) != len(f.Weeks) {
		t.Fatalf("weeks %d != %d", len(alpha.Weeks), len(f.Weeks))
	}
	last := f.Weeks[len(f.Weeks)-1]
	if !last.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)) || last.Weekday() != time.Monday {
		t.Errorf("last week start = %v", last)
	}
	cur, prev := alpha.Weeks[len(alpha.Weeks)-1], alpha.Weeks[len(alpha.Weeks)-2]
	if cur.Attempts != 2 || catCount(cur.Categories, "infra") != 2 {
		t.Errorf("current week = %+v", cur)
	}
	if prev.Attempts != 2 || catCount(prev.Categories, "content") != 1 || catCount(prev.Categories, UncategorizedFailure) != 1 {
		t.Errorf("previous week = %+v", prev)
	}
	sum := 0
	for _, w := range alpha.Weeks {
		sum += w.Attempts
	}
	if sum != alpha.Attempts {
		t.Errorf("weeks sum %d != attempts %d", sum, alpha.Attempts)
	}
}

func TestBuildFailures_TopSummariesNormalizeAndRank(t *testing.T) {
	f, err := BuildFailures(FailuresRequest{Window: 4 * 7 * 24 * time.Hour}, failRecords(), outcomesNow)
	if err != nil {
		t.Fatal(err)
	}
	alpha := failGroup(t, f, "alpha")
	if len(alpha.TopSummaries) != 2 {
		t.Fatalf("summaries = %+v", alpha.TopSummaries)
	}
	if top := alpha.TopSummaries[0]; top.Summary != "tmux gone" || top.Category != "infra" || top.Attempts != 2 {
		t.Errorf("whitespace variants must count as one summary: %+v", top)
	}

	long := strings.Repeat("x", 300)
	recs := []FailureRecord{{AutomatonID: "a", TaskID: "t", At: outcomesNow.Add(-time.Hour), Summary: long}}
	f, _ = BuildFailures(FailuresRequest{Window: 7 * 24 * time.Hour}, recs, outcomesNow)
	if s := f.Groups[0].TopSummaries[0].Summary; len([]rune(s)) != maxSummaryLen+1 || !strings.HasSuffix(s, "…") {
		t.Errorf("a long summary is cut: len=%d", len([]rune(s)))
	}
}

func TestBuildFailures_TopSummariesCapped(t *testing.T) {
	var recs []FailureRecord
	for i := 0; i < MaxTopSummaries+3; i++ {
		recs = append(recs, FailureRecord{AutomatonID: "a", TaskID: "t", At: outcomesNow.Add(-time.Hour), Category: "content", Summary: string(rune('a' + i))})
	}
	f, _ := BuildFailures(FailuresRequest{Window: 7 * 24 * time.Hour}, recs, outcomesNow)
	if got := len(f.Groups[0].TopSummaries); got != MaxTopSummaries {
		t.Errorf("top summaries = %d, want %d", got, MaxTopSummaries)
	}
}

func TestBuildFailures_ByAutomationAndAutomaton(t *testing.T) {
	f, err := BuildFailures(FailuresRequest{GroupBy: FailuresByAutomation, Window: 4 * 7 * 24 * time.Hour}, failRecords(), outcomesNow)
	if err != nil {
		t.Fatal(err)
	}
	n1 := failGroup(t, f, "n1")
	if n1.Label != "Nightly" || n1.Attempts != 3 || n1.Tasks != 2 || n1.Retries != 3 {
		t.Errorf("n1 = %+v", n1)
	}
	if g := failGroup(t, f, UnattributedKey); g.Attempts != 2 || g.Label != "" {
		t.Errorf("standalone Automata land under unattributed: %+v", g)
	}

	f, err = BuildFailures(FailuresRequest{GroupBy: FailuresByAutomaton, Window: 4 * 7 * 24 * time.Hour}, failRecords(), outcomesNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Groups) != 3 {
		t.Fatalf("groups = %+v", f.Groups)
	}
	if g := failGroup(t, f, "a1"); g.Label != "Alpha work" || g.Attempts != 3 {
		t.Errorf("a1 = %+v", g)
	}
	if f.Groups[0].Key != "a1" || f.Groups[1].Key != "a2" || f.Groups[2].Key != "a3" {
		t.Errorf("groups must sort by key: %+v", f.Groups)
	}
}

func TestBuildFailures_RetryCountKeepsHighestPerTask(t *testing.T) {
	at := outcomesNow.Add(-time.Hour)
	recs := []FailureRecord{
		{AutomatonID: "a", TaskID: "t", At: at, RetryCount: 1},
		{AutomatonID: "a", TaskID: "t", At: at, RetryCount: 3},
		{AutomatonID: "a", TaskID: "t", At: at, RetryCount: 2},
		{AutomatonID: "a", TaskID: "neg", At: at, RetryCount: -5},
	}
	f, _ := BuildFailures(FailuresRequest{Window: 7 * 24 * time.Hour}, recs, outcomesNow)
	if g := f.Groups[0]; g.Tasks != 2 || g.Retries != 3 || g.Attempts != 4 {
		t.Errorf("group = %+v", g)
	}
	if !strings.Contains(f.RetryNote, "once per task") {
		t.Errorf("retry note = %q", f.RetryNote)
	}
}

func TestBuildFailures_EmptyAndDefaults(t *testing.T) {
	f, err := BuildFailures(FailuresRequest{}, nil, outcomesNow)
	if err != nil {
		t.Fatal(err)
	}
	if f.Window != DefaultFailuresWindow.String() || len(f.Weeks) != 13 {
		t.Errorf("default window = %q with %d weeks", f.Window, len(f.Weeks))
	}
	raw, _ := json.Marshal(f)
	if !strings.Contains(string(raw), `"groups":[]`) || !strings.Contains(string(raw), `"retry_note"`) {
		t.Errorf("empty report must serialize groups as []: %s", raw)
	}
	if f.TotalAttempts != 0 || f.UndatedAttempts != 0 {
		t.Errorf("totals = %+v", f)
	}
	if _, err := BuildFailures(FailuresRequest{Window: time.Hour}, nil, time.Time{}); err != nil {
		t.Errorf("zero now reads the clock: %v", err)
	}
}

func TestBuildFailures_Validation(t *testing.T) {
	for name, req := range map[string]FailuresRequest{
		"group_by":        {GroupBy: "week"},
		"negative window": {Window: -time.Hour},
		"too many weeks":  {Window: 11 * 365 * 24 * time.Hour},
	} {
		if _, err := BuildFailures(req, nil, outcomesNow); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestFailureWeekStart(t *testing.T) {
	monday := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ in, want time.Time }{
		{monday, monday},
		{monday.Add(23*time.Hour + 59*time.Minute), monday},
		{time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC), monday}, // Sunday
		{time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), monday.AddDate(0, 0, 7)},
		{time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), monday.AddDate(0, 0, -7)},
	} {
		if got := failureWeekStart(c.in); !got.Equal(c.want) {
			t.Errorf("week of %v = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFailuresGroupByValid(t *testing.T) {
	for _, g := range []FailuresGroupBy{FailuresByProject, FailuresByAutomation, FailuresByAutomaton} {
		if !g.Valid() {
			t.Errorf("%q should be valid", g)
		}
	}
	if FailuresGroupBy("model").Valid() || FailuresGroupBy("").Valid() {
		t.Error("model and blank are not failure groupings")
	}
}
