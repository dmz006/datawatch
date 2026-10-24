package autonomous

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// Fixtures reuse the in-memory builders of the engine tests; nothing here reads
// the live store or the plans registry.

func prFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	peNearPtr(t, name, got, want)
}

func prHas(list []string, key string) bool {
	for _, k := range list {
		if k == key {
			return true
		}
	}
	return false
}

// prDoneRec builds a completed record with one completed task of the given
// length ending on the given day.
func prDoneRec(t *testing.T, id, created, start, end string) *PRD {
	return pePRD(t, id, PRDCompleted, created, peDone(t, start, end))
}

// prDrafts builds n draft records with no tasks.
func prDrafts(t *testing.T, n int) []*PRD {
	var out []*PRD
	for i := 0; i < n; i++ {
		out = append(out, pePRD(t, fmt.Sprintf("dddd%04d", i), PRDDraft, "2026-10-19T00:00:00Z"))
	}
	return out
}

// With the worked example: record c is running with one open task, so known is
// 1 record and 1 task. Five completed tasks' worth of rates are checked by hand
// below.
func TestProgressRemainingWorkedExample(t *testing.T) {
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})

	k := m.Remaining.Known
	if k.PRDs != 1 || k.Tasks != 1 || k.UndecomposedPRDs != 0 {
		t.Fatalf("known = %+v", k)
	}
	e := m.Remaining.Estimated
	if e.NewPRDs != 0 || e.UndecomposedPRDs != 0 || e.EstimatedTasks != 0 {
		t.Fatalf("estimated = %+v", e)
	}
	// Tasks per record: 2, 1 and 2.
	want := TasksPerPRD{Mean: 5.0 / 3.0, Median: 2, Min: 1, Max: 2, Samples: 3}
	if e.TasksPerPRD != want {
		t.Fatalf("tasks_per_prd = %+v, want %+v", e.TasksPerPRD, want)
	}
	if e.PerKind["feature"].Samples != 2 || e.PerKind["bug"].Samples != 1 || e.PerKind["other"].Samples != 0 {
		t.Fatalf("per_kind = %+v", e.PerKind)
	}
	if e.PerKind["bug"].Mean != 2 || e.PerKind["feature"].Mean != 1.5 {
		t.Fatalf("per_kind means = %+v", e.PerKind)
	}
	c := m.Remaining.Combined
	if c.PRDs != 1 || c.Tasks != 1 || c.Label != "known_plus_estimated" {
		t.Fatalf("combined = %+v", c)
	}

	// Rates: 2 records in the last three days over 53 active hours; 4 completed
	// tasks over 41 active hours; 3 tasks in the last day over 5 active hours;
	// 5.5 worker hours over 4 completed tasks.
	cp := m.Completion
	prFloat(t, "by_prd_rate", cp.ByPRDRateDays, 1.0/(2.0/(53.0/24.0)))
	prFloat(t, "by_task_rate", cp.ByTaskRateDays, 41.0/96.0)
	prFloat(t, "by_task_rate_24h", cp.ByTaskRate24hDays, 1.0/3.0*5.0/24.0)
	prFloat(t, "by_worker_hours", cp.ByWorkerHoursDays, 1.0*(5.5/4.0)/(5.5/(41.0/24.0)))
	prFloat(t, "low", cp.LowDays, 5.0/72.0)
	prFloat(t, "high", cp.HighDays, 53.0/48.0)
	prFloat(t, "calendar", cp.CalendarDays, (41.0/96.0)/(1.0-19.0/60.0))
	prFloat(t, "inactive calendar", m.Inactive.CalendarEstimateDays, (41.0/96.0)/(1.0-19.0/60.0))
	if got := strings.Join(cp.Drivers, ","); got != "utilization,sparse_history" {
		t.Fatalf("drivers = %q", got)
	}
	for _, key := range []string{"prd_rate", "task_rate", "task_rate_24h", "worker_hours"} {
		if cp.Method[key] == "" {
			t.Fatalf("method lacks %q: %v", key, cp.Method)
		}
	}
}

func TestProgressRemainingFiledAndUnfiledExpansion(t *testing.T) {
	cases := []struct {
		name string
		rows []BacklogRow
		want int
	}{
		{"unfiled single phase", []BacklogRow{{Open: true, Phases: 1}}, 1},
		{"unfiled phase count unset counts one", []BacklogRow{{Open: true}}, 1},
		{"unfiled multi phase", []BacklogRow{{Open: true, Phases: 4}}, 4},
		{"filed covers its phases", []BacklogRow{{Open: true, Phases: 1, PRDIDs: []string{"aaaa0001"}}}, 0},
		{"filed with phases left", []BacklogRow{{Open: true, Phases: 3, PRDIDs: []string{"aaaa0001"}}}, 2},
		{"filed with more records than phases", []BacklogRow{{Open: true, Phases: 1, PRDIDs: []string{"aaaa0001", "bbbb0002"}}}, 0},
		{"closed rows add nothing", []BacklogRow{{Open: false, Phases: 5}}, 0},
		{"rows add up", []BacklogRow{
			{Open: true, Phases: 2},
			{Open: true, Phases: 3, PRDIDs: []string{"aaaa0001"}},
			{Open: true},
			{Open: false, Phases: 9},
		}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ComputeProgress(ProgressInput{Backlog: tc.rows, Options: peOpts(t)})
			if got := m.Remaining.Estimated.NewPRDs; got != tc.want {
				t.Fatalf("new_prds = %d, want %d", got, tc.want)
			}
			if m.Remaining.Combined.PRDs != tc.want {
				t.Fatalf("combined prds = %d, want %d", m.Remaining.Combined.PRDs, tc.want)
			}
		})
	}
}

// Lesson: counting a registry row and the record it already links to made the
// remaining work look bigger than it was. A row with a record is counted once,
// through the record.
func TestProgressRemainingDoesNotDoubleCountRowWithRecord(t *testing.T) {
	rec := pePRD(t, "abcd0001", PRDRunning, "2026-10-19T00:00:00Z",
		peDone(t, "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"),
		peTask(t, TaskPending, "", ""))
	rows := []BacklogRow{{Number: "7", Item: "Has a record", Open: true, Phases: 1, PRDIDs: []string{"abcd0001"}}}
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{rec}, Backlog: rows, Options: peOpts(t)})

	if m.Remaining.Known.PRDs != 1 {
		t.Fatalf("known prds = %d", m.Remaining.Known.PRDs)
	}
	if m.Remaining.Estimated.NewPRDs != 0 {
		t.Fatalf("new prds = %d, want 0: the row already has a record", m.Remaining.Estimated.NewPRDs)
	}
	if m.Remaining.Combined.PRDs != 1 {
		t.Fatalf("combined prds = %d, want 1", m.Remaining.Combined.PRDs)
	}
}

// Lesson: a record with no tasks yet has an unknown size. It is labelled
// estimated and never added to the known task count.
func TestProgressRemainingRecordsWithNoTasksAreEstimatedNotKnown(t *testing.T) {
	prds := append(peWorked(t), prDrafts(t, 2)...)
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})

	k := m.Remaining.Known
	if k.PRDs != 3 || k.Tasks != 1 || k.UndecomposedPRDs != 2 {
		t.Fatalf("known = %+v: the empty records add no known tasks", k)
	}
	e := m.Remaining.Estimated
	if e.UndecomposedPRDs != 2 {
		t.Fatalf("estimated undecomposed = %d", e.UndecomposedPRDs)
	}
	peNear(t, "estimated tasks", e.EstimatedTasks, 2*5.0/3.0)
	peNear(t, "combined tasks", m.Remaining.Combined.Tasks, 1+2*5.0/3.0)
}

func TestProgressRemainingNoSamplesMakesTheEstimateNull(t *testing.T) {
	rows := []BacklogRow{{Open: true, Phases: 2}}
	m := ComputeProgress(ProgressInput{PRDs: prDrafts(t, 1), Backlog: rows, Options: peOpts(t)})

	e := m.Remaining.Estimated
	if e.TasksPerPRD.Samples != 0 || e.EstimatedTasks != 0 {
		t.Fatalf("estimate = %+v, want no samples and no estimated tasks", e)
	}
	if e.NewPRDs != 2 || e.UndecomposedPRDs != 1 {
		t.Fatalf("estimated counts = %+v", e)
	}
	if !prHas(m.NotMeasured, "tasks_per_prd") {
		t.Fatalf("not_measured = %v, want tasks_per_prd", m.NotMeasured)
	}
	if prHas(m.NotMeasured, "backlog") {
		t.Fatalf("not_measured = %v: the backlog was available", m.NotMeasured)
	}
}

func TestProgressRemainingMissingBacklogIsReported(t *testing.T) {
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})
	if m.BacklogAvailable || !prHas(m.NotMeasured, "backlog") {
		t.Fatalf("backlog_available=%v not_measured=%v", m.BacklogAvailable, m.NotMeasured)
	}
	if m.Remaining.Estimated.NewPRDs != 0 {
		t.Fatalf("new prds = %d with no backlog", m.Remaining.Estimated.NewPRDs)
	}
	if prHas(m.NotMeasured, "tasks_per_prd") {
		t.Fatalf("not_measured = %v: there are samples", m.NotMeasured)
	}
}

func TestProgressRemainingCancelledMergedRecordIsNotKnownWork(t *testing.T) {
	merged := pePRD(t, "eeee0001", PRDCancelled, "2026-10-19T00:00:00Z",
		peDone(t, "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"))
	abandoned := pePRD(t, "eeee0002", PRDCancelled, "2026-10-19T00:00:00Z",
		peTask(t, TaskPending, "", ""))
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{merged, abandoned}, Options: peOpts(t)})
	if m.Remaining.Known.PRDs != 0 || m.Remaining.Known.Tasks != 0 {
		t.Fatalf("known = %+v", m.Remaining.Known)
	}
}

// Each method has its own divisor; when it is zero that method is null and the
// others stay.
func TestProgressRemainingMethodNullHandling(t *testing.T) {
	open := func(id string) *PRD {
		return pePRD(t, id, PRDApproved, "2026-10-19T00:00:00Z", peTask(t, TaskPending, "", ""))
	}
	cases := []struct {
		name                            string
		prds                            []*PRD
		prd, task, task24h, workerHours bool // true = expected non-null
	}{
		{
			name: "nothing completed",
			prds: []*PRD{open("ffff0001")},
		},
		{
			name: "completed long ago: no recent window",
			prds: []*PRD{
				prDoneRec(t, "ffff0002", "2026-10-11T00:00:00Z", "2026-10-11T01:00:00Z", "2026-10-11T02:00:00Z"),
				open("ffff0001"),
			},
			task: true, workerHours: true,
		},
		{
			name: "completed task without times: no worker hours",
			prds: []*PRD{
				pePRD(t, "ffff0003", PRDCompleted, "2026-10-19T00:00:00Z", peDone(t, "", "")),
				open("ffff0001"),
			},
			task: true, task24h: true,
		},
		{
			name: "recent completion fills every method",
			prds: []*PRD{
				prDoneRec(t, "ffff0004", "2026-10-19T00:00:00Z", "2026-10-20T01:00:00Z", "2026-10-20T02:00:00Z"),
				open("ffff0001"),
			},
			prd: true, task: true, task24h: true, workerHours: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ComputeProgress(ProgressInput{PRDs: tc.prds, Options: peOpts(t)})
			c := m.Completion
			for _, chk := range []struct {
				name string
				got  *float64
				want bool
			}{
				{"by_prd_rate", c.ByPRDRateDays, tc.prd},
				{"by_task_rate", c.ByTaskRateDays, tc.task},
				{"by_task_rate_24h", c.ByTaskRate24hDays, tc.task24h},
				{"by_worker_hours", c.ByWorkerHoursDays, tc.workerHours},
			} {
				if (chk.got != nil) != chk.want {
					t.Errorf("%s non-null = %v, want %v", chk.name, chk.got != nil, chk.want)
				}
				if chk.got != nil && (math.IsNaN(*chk.got) || math.IsInf(*chk.got, 0)) {
					t.Errorf("%s = %v", chk.name, *chk.got)
				}
			}
			if !tc.prd && !tc.task && !tc.task24h && !tc.workerHours {
				if c.LowDays != nil || c.HighDays != nil || c.CalendarDays != nil {
					t.Errorf("low/high/calendar = %v/%v/%v, want all nil", c.LowDays, c.HighDays, c.CalendarDays)
				}
			}
		})
	}
}

func TestProgressRemainingLowAndHighAreMinAndMaxOfNonNullMethods(t *testing.T) {
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})
	c := m.Completion
	min, max := math.Inf(1), math.Inf(-1)
	n := 0
	for _, v := range []*float64{c.ByPRDRateDays, c.ByTaskRateDays, c.ByTaskRate24hDays, c.ByWorkerHoursDays} {
		if v == nil {
			continue
		}
		n++
		min, max = math.Min(min, *v), math.Max(max, *v)
	}
	if n != 4 {
		t.Fatalf("non-null methods = %d, want 4", n)
	}
	prFloat(t, "low", c.LowDays, min)
	prFloat(t, "high", c.HighDays, max)
	// Calendar days are a view of the task-rate figure, not a fifth method.
	if *c.CalendarDays <= *c.ByTaskRateDays {
		t.Fatalf("calendar %v should exceed the active-time figure %v", *c.CalendarDays, *c.ByTaskRateDays)
	}
}

func TestProgressRemainingCalendarDaysNeedAShareBelowNinetyFivePercent(t *testing.T) {
	opts := peOpts(t)
	rec := prDoneRec(t, "abab0001", "2026-10-10T00:00:00Z", "2026-10-10T00:00:00Z", "2026-10-10T01:00:00Z")
	open := pePRD(t, "abab0002", PRDApproved, "2026-10-10T00:00:00Z", peTask(t, TaskPending, "", ""))

	// Calendar time is 252 hours; the window excludes 250 of them.
	win := []InactiveWindow{{Start: peT(t, "2026-10-10T01:00:00Z"), End: peT(t, "2026-10-20T11:00:00Z"), Kind: "outage"}}
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{rec, open}, Windows: win, Options: opts})
	if m.Inactive.ShareOfCalendar < 0.95 {
		t.Fatalf("share = %v, fixture is wrong", m.Inactive.ShareOfCalendar)
	}
	if m.Completion.CalendarDays != nil || m.Inactive.CalendarEstimateDays != nil {
		t.Fatalf("calendar = %v / %v, want nil at a share of %v", m.Completion.CalendarDays, m.Inactive.CalendarEstimateDays, m.Inactive.ShareOfCalendar)
	}
	if m.Completion.ByTaskRateDays == nil {
		t.Fatal("the active-time figure should still be set")
	}

	m = ComputeProgress(ProgressInput{PRDs: []*PRD{rec, open}, Options: opts})
	if m.Completion.CalendarDays == nil {
		t.Fatalf("calendar days nil with share %v", m.Inactive.ShareOfCalendar)
	}
}

func TestProgressRemainingDriversThresholds(t *testing.T) {
	// completedN builds n completed records with one short task each.
	completedN := func(t *testing.T, n int) []*PRD {
		var out []*PRD
		for i := 0; i < n; i++ {
			out = append(out, prDoneRec(t, fmt.Sprintf("c0c0%04d", i), "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"))
		}
		return out
	}
	withTasks := func(t *testing.T, id string, n int) *PRD {
		var ts []Task
		for i := 0; i < n; i++ {
			ts = append(ts, peDone(t, "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"))
		}
		return pePRD(t, id, PRDCompleted, "2026-10-19T00:00:00Z", ts...)
	}
	busy := func(t *testing.T) ProgressInput {
		// One lane, ten worked hours in a half day: utilization 0.83.
		o := peOpts(t)
		o.Lanes = 1
		o.Now = peT(t, "2026-10-19T12:00:00Z")
		rec := prDoneRec(t, "b0b00001", "2026-10-19T00:00:00Z", "2026-10-19T00:00:00Z", "2026-10-19T10:00:00Z")
		return ProgressInput{PRDs: []*PRD{rec}, Options: o}
	}

	cases := []struct {
		name   string
		in     func(t *testing.T) ProgressInput
		driver string
		want   bool
	}{
		{"low utilization", func(t *testing.T) ProgressInput { return ProgressInput{PRDs: peWorked(t), Options: peOpts(t)} }, "utilization", true},
		{"busy lanes", busy, "utilization", false},
		{"even mix", func(t *testing.T) ProgressInput {
			return ProgressInput{PRDs: []*PRD{withTasks(t, "a0a00001", 2), withTasks(t, "a0a00002", 2), withTasks(t, "a0a00003", 3)}, Options: peOpts(t)}
		}, "large_prd_mix", false},
		{"one huge record", func(t *testing.T) ProgressInput {
			return ProgressInput{PRDs: []*PRD{withTasks(t, "a0a00001", 1), withTasks(t, "a0a00002", 1), withTasks(t, "a0a00003", 1), withTasks(t, "a0a00004", 5)}, Options: peOpts(t)}
		}, "large_prd_mix", true},
		{"mostly estimated", func(t *testing.T) ProgressInput {
			known := pePRD(t, "d0d00001", PRDRunning, "2026-10-19T00:00:00Z",
				peDone(t, "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"), peTask(t, TaskPending, "", ""))
			return ProgressInput{PRDs: append([]*PRD{known}, prDrafts(t, 6)...), Options: peOpts(t)}
		}, "undecomposed_backlog", true},
		{"all known", func(t *testing.T) ProgressInput {
			known := pePRD(t, "d0d00001", PRDRunning, "2026-10-19T00:00:00Z",
				peDone(t, "2026-10-19T01:00:00Z", "2026-10-19T02:00:00Z"), peTask(t, TaskPending, "", ""))
			return ProgressInput{PRDs: []*PRD{known}, Options: peOpts(t)}
		}, "undecomposed_backlog", false},
		{"four completed", func(t *testing.T) ProgressInput { return ProgressInput{PRDs: completedN(t, 4), Options: peOpts(t)} }, "sparse_history", true},
		{"five completed", func(t *testing.T) ProgressInput { return ProgressInput{PRDs: completedN(t, 5), Options: peOpts(t)} }, "sparse_history", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ComputeProgress(tc.in(t))
			if got := prHas(m.Completion.Drivers, tc.driver); got != tc.want {
				t.Fatalf("driver %q present = %v, want %v (drivers %v)", tc.driver, got, tc.want, m.Completion.Drivers)
			}
		})
	}
}

func TestProgressRemainingEmptyStoreIsAllNullAndFinite(t *testing.T) {
	m := ComputeProgress(ProgressInput{Options: peOpts(t)})

	r := m.Remaining
	if r.Known != (RemainingKnown{}) || r.Estimated.NewPRDs != 0 || r.Estimated.EstimatedTasks != 0 || r.Combined.PRDs != 0 || r.Combined.Tasks != 0 {
		t.Fatalf("remaining = %+v", r)
	}
	if r.Combined.Label != "known_plus_estimated" {
		t.Fatalf("label = %q", r.Combined.Label)
	}
	c := m.Completion
	for name, v := range map[string]*float64{
		"by_prd_rate": c.ByPRDRateDays, "by_task_rate": c.ByTaskRateDays, "by_task_rate_24h": c.ByTaskRate24hDays,
		"by_worker_hours": c.ByWorkerHoursDays, "low": c.LowDays, "high": c.HighDays, "calendar": c.CalendarDays,
		"inactive calendar": m.Inactive.CalendarEstimateDays,
	} {
		if v != nil {
			t.Errorf("%s = %v, want nil", name, *v)
		}
	}
	if c.Drivers == nil || len(c.Method) != 4 {
		t.Fatalf("drivers=%v method=%v", c.Drivers, c.Method)
	}
	for _, key := range []string{"backlog", "tasks_per_prd"} {
		if !prHas(m.NotMeasured, key) {
			t.Errorf("not_measured = %v, lacks %q", m.NotMeasured, key)
		}
	}
	if len(r.Estimated.PerKind) != 3 {
		t.Fatalf("per_kind = %v", r.Estimated.PerKind)
	}

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, bad := range []string{"NaN", "Inf"} {
		if strings.Contains(s, bad) {
			t.Fatalf("json holds %s: %s", bad, s)
		}
	}
	for _, key := range []string{`"by_prd_rate_days":null`, `"by_task_rate_days":null`, `"low_days":null`, `"high_days":null`, `"calendar_days":null`, `"calendar_estimate_days":null`} {
		if !strings.Contains(s, key) {
			t.Fatalf("json lacks %s:\n%s", key, s)
		}
	}
}

func TestProgressRemainingJSONShapeIsStable(t *testing.T) {
	rows := []BacklogRow{{Number: "1", Item: "Not filed", Open: true, Phases: 2}}
	in := ProgressInput{PRDs: append(peWorked(t), prDrafts(t, 1)...), Backlog: rows, Options: peOpts(t)}
	m := ComputeProgress(in)

	first, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back ProgressMetrics
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("marshal, unmarshal, marshal changed the output:\n%s\n%s", first, second)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(first, &top); err != nil {
		t.Fatal(err)
	}
	var rem map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["remaining"], &rem); err != nil {
		t.Fatal(err)
	}
	wantRemaining := map[string][]string{
		"known":     {"prds", "tasks", "undecomposed_prds"},
		"estimated": {"new_prds", "undecomposed_prds", "estimated_tasks", "tasks_per_prd", "per_kind"},
		"combined":  {"prds", "tasks", "label"},
	}
	for group, keys := range wantRemaining {
		for _, k := range keys {
			if _, ok := rem[group][k]; !ok {
				t.Errorf("remaining.%s lacks %q", group, k)
			}
		}
	}
	var comp map[string]json.RawMessage
	if err := json.Unmarshal(top["completion"], &comp); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"by_prd_rate_days", "by_task_rate_days", "by_task_rate_24h_days", "by_worker_hours_days", "low_days", "high_days", "drivers", "method", "calendar_days"} {
		if _, ok := comp[k]; !ok {
			t.Errorf("completion lacks %q", k)
		}
	}
}

func TestProgressRemainingMethodTextAvoidsInternalTerms(t *testing.T) {
	m := ComputeProgress(ProgressInput{Options: peOpts(t)})
	for k, v := range m.Completion.Method {
		if strings.Contains(strings.ToLower(v), "prd") {
			t.Errorf("method %q text uses an internal term: %q", k, v)
		}
	}
}

func TestProgressRemainingDoesNotChangeItsInput(t *testing.T) {
	rows := []BacklogRow{{Number: "1", Open: true, Phases: 3}}
	prds := peWorked(t)
	before, _ := json.Marshal(ProgressInput{PRDs: prds, Backlog: rows, Options: peOpts(t)})
	ComputeProgress(ProgressInput{PRDs: prds, Backlog: rows, Options: peOpts(t)})
	after, _ := json.Marshal(ProgressInput{PRDs: prds, Backlog: rows, Options: peOpts(t)})
	if string(before) != string(after) {
		t.Fatal("the computation changed its input")
	}
}
