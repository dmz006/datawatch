package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }

// snapMetrics returns metrics whose completion range is [low, high] days
// (nil = no range) and whose completed count is done.
func snapMetrics(low, high *float64, done int) ProgressMetrics {
	m := ProgressMetrics{CompletedPRDs: done}
	m.Completion.LowDays = low
	m.Completion.HighDays = high
	return m
}

func newSnapStore(t *testing.T) (*ProgressSnapshotStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, skipped, err := NewProgressSnapshotStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Fatalf("fresh store skipped %d lines", skipped)
	}
	return s, dir
}

func day(d int, hour int) time.Time {
	return time.Date(2026, 10, d, hour, 0, 0, 0, time.UTC)
}

func TestProgressSnapshotMissingFileIsEmpty(t *testing.T) {
	s, dir := newSnapStore(t)
	if got := s.List(30, day(20, 12)); len(got) != 0 {
		t.Fatalf("want empty store, got %d", len(got))
	}
	if _, err := os.Stat(filepath.Join(dir, "progress-snapshots.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("an empty store must not create the file: %v", err)
	}
}

func TestProgressSnapshotDailyIsIdempotentPerDay(t *testing.T) {
	s, _ := newSnapStore(t)
	if err := s.RecordDaily(day(20, 1), snapMetrics(nil, nil, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDaily(day(20, 23), snapMetrics(nil, nil, 2)); err != nil {
		t.Fatal(err)
	}
	got := s.List(30, day(20, 23))
	if len(got) != 1 {
		t.Fatalf("want 1 daily snapshot for the day, got %d", len(got))
	}
	if got[0].Metrics.CompletedPRDs != 2 || got[0].Reason != "daily" || got[0].Date != "2026-10-20" {
		t.Fatalf("second record must replace the first: %+v", got[0])
	}
	if !s.HasDaily(day(20, 5)) || s.HasDaily(day(21, 0)) {
		t.Fatal("HasDaily must follow the UTC date")
	}
}

func TestProgressSnapshotDateIsUTC(t *testing.T) {
	s, _ := newSnapStore(t)
	zone := time.FixedZone("late", -10*3600)
	// 2026-10-20 20:00 at -10h is 2026-10-21 06:00 UTC.
	if err := s.RecordDaily(time.Date(2026, 10, 20, 20, 0, 0, 0, zone), ProgressMetrics{}); err != nil {
		t.Fatal(err)
	}
	got := s.List(0, day(25, 0))
	if len(got) != 1 || got[0].Date != "2026-10-21" || got[0].TakenAt.Location() != time.UTC {
		t.Fatalf("snapshot must be dated and stamped in UTC: %+v", got)
	}
}

func TestProgressSnapshotMilestoneAppends(t *testing.T) {
	s, _ := newSnapStore(t)
	if err := s.RecordDaily(day(20, 1), ProgressMetrics{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.RecordMilestone(day(20, 2), snapMetrics(nil, nil, i), "completed:abc"); err != nil {
			t.Fatal(err)
		}
	}
	got := s.List(30, day(20, 3))
	if len(got) != 3 {
		t.Fatalf("want daily + 2 milestones, got %d", len(got))
	}
	if got[1].Reason != "milestone" || got[1].Label != "completed:abc" || got[2].Metrics.CompletedPRDs != 1 {
		t.Fatalf("milestones must append in order: %+v", got)
	}
	// A daily record the same day must not replace a milestone.
	if err := s.RecordDaily(day(20, 4), ProgressMetrics{}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(30, day(20, 5))); n != 3 {
		t.Fatalf("daily replace must leave milestones alone, got %d", n)
	}
}

func TestProgressSnapshotListOrderAndWindow(t *testing.T) {
	s, _ := newSnapStore(t)
	// Recorded out of order on purpose.
	for _, d := range []int{18, 10, 20, 15} {
		if err := s.RecordDaily(day(d, 6), snapMetrics(nil, nil, d)); err != nil {
			t.Fatal(err)
		}
	}
	now := day(20, 12)
	all := s.List(0, now)
	var dates []string
	for _, e := range all {
		dates = append(dates, e.Date)
	}
	if strings.Join(dates, ",") != "2026-10-10,2026-10-15,2026-10-18,2026-10-20" {
		t.Fatalf("want oldest first, got %v", dates)
	}
	// days=1 is today only; days=3 reaches back to 10-18; 10-17 would be day 4.
	if got := s.List(1, now); len(got) != 1 || got[0].Date != "2026-10-20" {
		t.Fatalf("days=1: %+v", got)
	}
	if got := s.List(3, now); len(got) != 2 || got[0].Date != "2026-10-18" {
		t.Fatalf("days=3: %+v", got)
	}
	if got := s.List(6, now); len(got) != 3 || got[0].Date != "2026-10-15" {
		t.Fatalf("days=6 must include the boundary day: %+v", got)
	}
	if got := s.List(-5, now); len(got) != 4 {
		t.Fatalf("a non-positive window means every snapshot, got %d", len(got))
	}
}

func TestProgressSnapshotPrune(t *testing.T) {
	s, _ := newSnapStore(t)
	for _, d := range []int{1, 8, 14, 15, 20} {
		if err := s.RecordDaily(day(d, 6), ProgressMetrics{}); err != nil {
			t.Fatal(err)
		}
	}
	now := day(20, 12)
	// Retention 5 keeps dates >= 10-15.
	if n := s.Prune(5, now); n != 3 {
		t.Fatalf("want 3 pruned, got %d", n)
	}
	got := s.List(0, now)
	if len(got) != 2 || got[0].Date != "2026-10-15" {
		t.Fatalf("after prune: %+v", got)
	}
	if n := s.Prune(5, now); n != 0 {
		t.Fatalf("second prune must drop nothing, got %d", n)
	}
	// 0 keeps the default of 90 days.
	if n := s.Prune(0, now.AddDate(0, 0, 80)); n != 0 {
		t.Fatalf("default retention is 90 days, pruned %d at 80", n)
	}
	if n := s.Prune(0, now.AddDate(0, 0, 100)); n != 2 {
		t.Fatalf("default retention must prune past 90 days, pruned %d", n)
	}
}

func TestProgressSnapshotPrunePersists(t *testing.T) {
	s, dir := newSnapStore(t)
	_ = s.RecordDaily(day(1, 6), ProgressMetrics{})
	_ = s.RecordDaily(day(20, 6), ProgressMetrics{})
	s.Prune(5, day(20, 12))
	re, _, err := NewProgressSnapshotStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := re.List(0, day(20, 12)); len(got) != 1 || got[0].Date != "2026-10-20" {
		t.Fatalf("prune must reach the file: %+v", got)
	}
}

func TestProgressSnapshotCorruptLinesSkipped(t *testing.T) {
	s, dir := newSnapStore(t)
	_ = s.RecordDaily(day(19, 6), snapMetrics(nil, nil, 1))
	_ = s.RecordDaily(day(20, 6), snapMetrics(nil, nil, 2))
	path := filepath.Join(dir, "progress-snapshots.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// One garbage line, one valid JSON object with no date, one blank line.
	bad := string(data) + "{not json\n{\"reason\":\"daily\"}\n\n"
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	re, skipped, err := NewProgressSnapshotStore(dir)
	if err != nil {
		t.Fatalf("a corrupt line must not fail the load: %v", err)
	}
	if skipped != 2 {
		t.Fatalf("want 2 skipped lines, got %d", skipped)
	}
	if got := re.List(0, day(20, 12)); len(got) != 2 {
		t.Fatalf("good lines must survive, got %d", len(got))
	}
	// The next write drops the corrupt lines.
	_ = re.RecordMilestone(day(20, 8), ProgressMetrics{}, "x")
	re2, skipped, _ := NewProgressSnapshotStore(dir)
	if skipped != 0 || len(re2.List(0, day(20, 12))) != 3 {
		t.Fatalf("rewrite must clean the file: skipped=%d", skipped)
	}
}

func TestProgressSnapshotReloadFromDisk(t *testing.T) {
	s, dir := newSnapStore(t)
	m := snapMetrics(fp(3), fp(9), 7)
	if err := s.RecordDaily(day(19, 6), m); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMilestone(day(19, 9), snapMetrics(nil, nil, 8), "completed:ab12cd34"); err != nil {
		t.Fatal(err)
	}
	re, skipped, err := NewProgressSnapshotStore(dir)
	if err != nil || skipped != 0 {
		t.Fatalf("reload: err=%v skipped=%d", err, skipped)
	}
	got := re.List(0, day(20, 0))
	if len(got) != 2 {
		t.Fatalf("want 2 after restart, got %d", len(got))
	}
	if got[0].Metrics.Completion.LowDays == nil || *got[0].Metrics.Completion.HighDays != 9 || got[0].Metrics.CompletedPRDs != 7 {
		t.Fatalf("metrics must round trip: %+v", got[0].Metrics)
	}
	if got[1].Metrics.Completion.LowDays != nil {
		t.Fatal("a null range must stay null")
	}
	if !re.HasDaily(day(19, 20)) {
		t.Fatal("reloaded store must know the daily snapshot")
	}
}

func TestProgressSnapshotJSONTags(t *testing.T) {
	s, dir := newSnapStore(t)
	_ = s.RecordMilestone(day(19, 9), ProgressMetrics{}, "lbl")
	data, _ := os.ReadFile(filepath.Join(dir, "progress-snapshots.jsonl"))
	for _, key := range []string{`"date":"2026-10-19"`, `"taken_at":`, `"reason":"milestone"`, `"label":"lbl"`, `"metrics":`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("missing %s in %s", key, data)
		}
	}
}

func TestProgressSnapshotWriteFailureLeavesMemoryUnchanged(t *testing.T) {
	s, dir := newSnapStore(t)
	_ = s.RecordDaily(day(19, 6), ProgressMetrics{})
	// Replace the file by a directory so the rewrite fails.
	path := filepath.Join(dir, "progress-snapshots.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDaily(day(20, 6), ProgressMetrics{}); err == nil {
		t.Fatal("want a write error")
	}
	if got := s.List(0, day(20, 12)); len(got) != 1 {
		t.Fatalf("failed write must not change memory, got %d", len(got))
	}
	if n := s.Prune(1, day(25, 12)); n != 0 {
		t.Fatalf("failed prune must report 0, got %d", n)
	}
}

func TestProgressTrendDirection(t *testing.T) {
	mk := func(ranges ...[2]*float64) []ProgressSnapshot {
		var out []ProgressSnapshot
		for i, r := range ranges {
			out = append(out, ProgressSnapshot{Date: day(10+i, 0).Format("2006-01-02"), Reason: "daily", Metrics: snapMetrics(r[0], r[1], i)})
		}
		return out
	}
	cases := []struct {
		name  string
		snaps []ProgressSnapshot
		want  string
	}{
		{"converging", mk([2]*float64{fp(5), fp(15)}, [2]*float64{fp(6), fp(12)}, [2]*float64{fp(7), fp(10)}), "converging"},
		{"widening", mk([2]*float64{fp(5), fp(10)}, [2]*float64{fp(5), fp(14)}), "widening"},
		{"flat inside tolerance", mk([2]*float64{fp(5), fp(15)}, [2]*float64{fp(5), fp(14.5)}, [2]*float64{fp(5), fp(15.5)}), "flat"},
		{"exactly ten percent is flat", mk([2]*float64{fp(0), fp(10)}, [2]*float64{fp(0), fp(9)}), "flat"},
		{"shrank just over ten percent", mk([2]*float64{fp(0), fp(10)}, [2]*float64{fp(0), fp(8.9)}), "converging"},
		{"no points", nil, "unknown"},
		{"one point", mk([2]*float64{fp(1), fp(2)}), "unknown"},
		{"two points but one range", mk([2]*float64{fp(1), fp(2)}, [2]*float64{nil, nil}), "unknown"},
		{"half a range is no range", mk([2]*float64{fp(1), nil}, [2]*float64{fp(1), fp(4)}), "unknown"},
		{"nulls in the middle are skipped", mk([2]*float64{fp(5), fp(15)}, [2]*float64{nil, nil}, [2]*float64{fp(6), fp(8)}), "converging"},
		{"zero width stays flat", mk([2]*float64{fp(5), fp(5)}, [2]*float64{fp(4), fp(4)}), "flat"},
		{"growth from zero width widens", mk([2]*float64{fp(5), fp(5)}, [2]*float64{fp(4), fp(9)}), "widening"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Trend(c.snaps)
			if got.Direction != c.want {
				t.Fatalf("direction = %s, want %s", got.Direction, c.want)
			}
			if len(got.Points) != len(c.snaps) {
				t.Fatalf("one point per snapshot: %d vs %d", len(got.Points), len(c.snaps))
			}
		})
	}
}

func TestProgressTrendPointsAndNulls(t *testing.T) {
	m := ProgressMetrics{CompletedPRDs: 4}
	m.Remaining.Known.Tasks = 12
	m.Remaining.Estimated.EstimatedTasks = 30.5
	m.Throughput.PRDsPerDay3d = 2.25
	m.Utilization.Ratio = 0.8
	m.Completion.LowDays = fp(3)
	m.Completion.HighDays = fp(9)
	snaps := []ProgressSnapshot{
		{Date: "2026-10-19", Reason: "daily", Metrics: m},
		{Date: "2026-10-20", Reason: "milestone", Metrics: ProgressMetrics{}},
	}
	tr := Trend(snaps)
	p := tr.Points[0]
	if p.Date != "2026-10-19" || p.Reason != "daily" || *p.CompletionLowDays != 3 || *p.CompletionHighDays != 9 ||
		p.KnownTasks != 12 || p.EstimatedTasks != 30.5 || p.Throughput3d != 2.25 || p.Utilization != 0.8 || p.CompletedPRDs != 4 {
		t.Fatalf("point mapping: %+v", p)
	}
	q := tr.Points[1]
	if q.CompletionLowDays != nil || q.CompletionHighDays != nil || q.Reason != "milestone" {
		t.Fatalf("nulls must stay null: %+v", q)
	}
	if tr.WidthChangePercent != nil {
		t.Fatal("no width change with a single range")
	}
	data, err := jsonMarshalForTest(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `"completion_low_days":null`) || !strings.Contains(data, `"direction":"unknown"`) {
		t.Fatalf("null must serialize as null: %s", data)
	}
}

func TestProgressTrendWidthChangePercent(t *testing.T) {
	snaps := []ProgressSnapshot{
		{Date: "2026-10-10", Metrics: snapMetrics(fp(0), fp(10), 0)},
		{Date: "2026-10-11", Metrics: snapMetrics(fp(0), fp(5), 0)},
	}
	tr := Trend(snaps)
	if tr.WidthChangePercent == nil || *tr.WidthChangePercent != -50 {
		t.Fatalf("want -50, got %v", tr.WidthChangePercent)
	}
}

func jsonMarshalForTest(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
