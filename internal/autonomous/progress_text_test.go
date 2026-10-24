package autonomous

import (
	"strings"
	"testing"
	"time"
)

func ptrF(v float64) *float64 { return &v }

// progressTextFixture is a fixed metrics value with every field set.
func progressTextFixture() ProgressMetrics {
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	return ProgressMetrics{
		GeneratedAt:  at(23, 12),
		Since:        at(1, 0),
		Scope:        "all",
		CalendarDays: 22.5,
		ActiveDays:   20.0,
		Lanes:        5,
		Groups: map[string]ProgressGroup{
			"feature": {PRDs: 10, Done: 6, Running: 2, Waiting: 1, Blocked: 1, Tasks: 40, TasksDone: 24, TasksLeft: 16,
				WorkHours: 30, MedianPRDWallHours: ptrF(5), MeanTaskHours: ptrF(1.5)},
			"bug":   {PRDs: 4, Done: 3, Waiting: 1, Tasks: 8, TasksDone: 6, TasksLeft: 2, WorkHours: 60, MedianPRDWallHours: ptrF(60), MeanTaskHours: ptrF(10)},
			"other": {},
		},
		Totals: ProgressGroup{PRDs: 14, Done: 9, Running: 2, Waiting: 2, Blocked: 1, Tasks: 48, TasksDone: 30, TasksLeft: 18, WorkHours: 90,
			MedianPRDWallHours: ptrF(12), MeanTaskHours: ptrF(3)},
		RunningPRDs: 2,
		Throughput:  ProgressThroughput{PRDsPerDay3d: 1.5, TasksPerDay: 4.5},
		Utilization: ProgressUtilization{WorkerHoursPerDay: 12, Ratio: 0.1},
		LeadTime:    ProgressLeadTime{CreatedToApprovedMedianHours: ptrF(2), RunMedianHours: ptrF(50)},
		Inactive:    ProgressInactive{Windows: []InactiveWindow{{Start: at(5, 2), End: at(5, 9), Kind: "inactive"}, {Start: at(9, 20), End: at(10, 6), Kind: "outage", Reason: "AB123 host down"}}, ExcludedHours: 17, ShareOfCalendar: 0.03},
		Remaining: RemainingWork{
			Known:     RemainingKnown{PRDs: 5, Tasks: 18, UndecomposedPRDs: 1},
			Estimated: RemainingEstimated{NewPRDs: 3, EstimatedTasks: 21, TasksPerPRD: TasksPerPRD{Mean: 7, Median: 5, Samples: 9}},
			Combined:  RemainingCombined{PRDs: 8, Tasks: 39, Label: combinedLabel},
		},
		Completion: CompletionRange{
			ByPRDRateDays: ptrF(5.3), ByTaskRateDays: ptrF(8.7), ByWorkerHoursDays: ptrF(7.1),
			LowDays: ptrF(5.3), HighDays: ptrF(8.7), CalendarDays: ptrF(9.0),
			Drivers: []string{driverUtilization, driverSparse},
			Method:  map[string]string{methodPRDRate: "Assumes Automata finish at the rate of the last three active days."},
		},
		UnfiledRows:      []BacklogRow{{Item: "AB12 Add the export button"}, {Item: "Write the guide"}},
		BacklogAvailable: true,
		NotMeasured:      []string{},
	}
}

const progressTextGolden = `Progress since 2026-10-01: 22.5 calendar days, 20.0 active days after excluding 2 inactive windows of 17 h; 5 lanes
          Automata  done   run   wait blocked |  tasks   done  left |      work       median    mean/task
feature         10     6     2      1       1 |     40     24    16 |    30.0 h        5.0 h        1.5 h
bug              4     3     0      1       0 |      8      6     2 |     2.5 d        2.5 d       10.0 h
total           14     9     2      2       1 |     48     30    18 |     3.8 d       12.0 h        3.0 h
median = median wall time of a finished Automaton; mean/task = mean time of a finished task

LEFT  known: 5 Automata not done (2 running, 3 waiting or blocked) with 18 tasks still open; 1 not decomposed yet
      estimated: 3 Automata not filed yet; about 21 tasks (7.0 tasks per Automaton)
      combined estimate: about 8 Automata and about 39 tasks (known plus estimated)
ETA   5.3 to 8.7 active days (estimate; a range, not a promise)
      by Automaton rate: 5.3 days - Assumes Automata finish at the rate of the last three active days.
      by task rate: 8.7 days
      by task rate, last 24 h: not measured
      by worker hours: 7.1 days
      throughput 1.5 Automata/day (last 3 active days), 4.5 tasks/day; workers did 12.0 h/day = 10% of 5 lanes
      drivers: workers were idle much of the time, so the rate may rise; few
               Automata have finished, so every rate is thin
LEAD  median created to approved 2.0 h, approved to first task not measured, running 2.1 d, created to last task done not measured
WAITS 2 inactive windows, 17 h = 3% of calendar time (left out of every rate above, never hidden); calendar view if the same share recurs: about 9.0 days
      10-05 02:00 -> 10-05 09:00 UTC  7.0 h  [inactive]
      10-09 20:00 -> 10-10 06:00 UTC  10.0 h  [outage] host down
UNFILED ROWS (first 2 of 2):
  - Add the export button
  - Write the guide
`

func TestProgressTextGolden(t *testing.T) {
	got := RenderProgressText(progressTextFixture())
	if got != progressTextGolden {
		t.Fatalf("text differs from the golden string\n--- got ---\n%s\n--- want ---\n%s", got, progressTextGolden)
	}
}

func TestProgressTextNullsAreNotMeasured(t *testing.T) {
	m := ProgressMetrics{Groups: map[string]ProgressGroup{"feature": {}, "bug": {}, "other": {}}, Lanes: 5}
	got := RenderProgressText(m)
	for _, want := range []string{
		"ETA   not measured (no rate measured yet)",
		"by task rate: not measured",
		"LEAD  median created to approved not measured",
		"UNFILED ROWS not measured (no backlog file was read)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "NaN") || strings.Contains(got, "<nil>") || strings.Contains(got, "0xc") {
		t.Errorf("null leaked into the text:\n%s", got)
	}
	if strings.Contains(got, "\nother ") {
		t.Errorf("empty other row should be left out:\n%s", got)
	}
}

func TestProgressTextOtherRowShownWhenNonEmpty(t *testing.T) {
	m := progressTextFixture()
	m.Groups["other"] = ProgressGroup{PRDs: 2, Done: 1, Tasks: 3, TasksDone: 1, TasksLeft: 2}
	if got := RenderProgressText(m); !strings.Contains(got, "\nother ") {
		t.Errorf("other row missing:\n%s", got)
	}
}

func TestProgressTextScopeLine(t *testing.T) {
	m := progressTextFixture()
	m.Scope = "automation:a1"
	if got := RenderProgressText(m); !strings.Contains(got, "Scope: automation:a1\n") {
		t.Errorf("scope line missing:\n%s", got)
	}
}

func TestProgressTextWrapsLongItemText(t *testing.T) {
	long := "AB99 " + strings.Repeat("alpha beta gamma delta ", 10)
	m := progressTextFixture()
	m.UnfiledRows = []BacklogRow{{Item: long}}
	got := RenderProgressText(m)
	if strings.Contains(got, "AB99") {
		t.Errorf("tracker token not stripped:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "UNFILED ROWS") {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("no unfiled section:\n%s", got)
	}
	item := lines[start+1:]
	if len(item) < 3 {
		t.Fatalf("long item was not wrapped onto several lines: %q", item)
	}
	for _, l := range item {
		if len([]rune(l)) > progressTextWrapWidth {
			t.Errorf("line longer than %d: %q", progressTextWrapWidth, l)
		}
	}
	if !strings.HasPrefix(item[0], "  - alpha beta") || !strings.HasPrefix(item[1], "    ") {
		t.Errorf("wrapped item is not indented as a list entry: %q", item[:2])
	}
}

func TestProgressTextCapsUnfiledRowsAtFourteen(t *testing.T) {
	m := progressTextFixture()
	m.UnfiledRows = nil
	for i := 0; i < 20; i++ {
		m.UnfiledRows = append(m.UnfiledRows, BacklogRow{Item: "item number " + string(rune('a'+i))})
	}
	got := RenderProgressText(m)
	if !strings.Contains(got, "UNFILED ROWS (first 14 of 20):") {
		t.Errorf("header does not say 14 of 20:\n%s", got)
	}
	if strings.Count(got, "\n  - ") != 14 {
		t.Errorf("want 14 listed rows, got %d", strings.Count(got, "\n  - "))
	}
}

func TestProgressTextItemTextStripsLeadingToken(t *testing.T) {
	for in, want := range map[string]string{
		"AB123 Add the thing":  "Add the thing",
		"AB123: Add the thing": "Add the thing",
		"Add the AB123 thing":  "Add the AB123 thing",
		"  spaced   out  ":     "spaced out",
		"":                     "",
	} {
		if got := progressItemText(in); got != want {
			t.Errorf("progressItemText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressTextNoInternalTerm(t *testing.T) {
	got := strings.ToLower(RenderProgressText(progressTextFixture()))
	if strings.Contains(got, "prd") {
		t.Errorf("operator text names the internal term:\n%s", got)
	}
}

func TestProgressTextTrend(t *testing.T) {
	if got := RenderTrendText(nil); got != "No progress snapshots saved yet.\n" {
		t.Errorf("empty trend = %q", got)
	}
	pts := []ProgressTrendPoint{
		{Date: "2026-10-21", Reason: "daily", CompletionLowDays: ptrF(5), CompletionHighDays: ptrF(9), KnownTasks: 20, EstimatedTasks: 10, Throughput3d: 1.5, Utilization: 0.25, CompletedPRDs: 7},
		{Date: "2026-10-22", Reason: "manual", KnownTasks: 18, CompletedPRDs: 8},
	}
	want := "date       reason     low days high days  known   est. tasks/day  util completed\n" +
		"2026-10-21 daily           5.0       9.0     20     10       1.5   25%         7\n" +
		"2026-10-22 manual            -         -     18      0       0.0    0%         8\n"
	if got := RenderTrendText(pts); got != want {
		t.Errorf("trend text\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	reply := RenderTrendReply(30, "converging", pts)
	if !strings.HasPrefix(reply, "Progress trend over the last 30 days: completion range converging\n") {
		t.Errorf("reply header: %q", reply)
	}
	if !strings.HasPrefix(RenderTrendReply(7, "", nil), "Progress trend over the last 7 days: completion range unknown\n") {
		t.Error("empty direction should read unknown")
	}
}
