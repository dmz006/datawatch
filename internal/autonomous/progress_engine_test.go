package autonomous

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// Fixtures for the engine tests. Every record is built in memory; nothing here
// reads the live store or the plans registry.

func peT(t *testing.T, s string) time.Time {
	t.Helper()
	v, ok := ParseProgressTime(s)
	if !ok {
		t.Fatalf("parse %q failed", s)
	}
	return v
}

func peP(v time.Time) *time.Time { return &v }

// peTask builds a task. start and end are ISO strings, "" meaning not set.
func peTask(t *testing.T, status TaskStatus, start, end string) Task {
	t.Helper()
	tk := Task{Status: status}
	if start != "" {
		tk.StartedAt = peP(peT(t, start))
	}
	if end != "" {
		tk.CompletedAt = peP(peT(t, end))
	}
	return tk
}

func peDone(t *testing.T, start, end string) Task { return peTask(t, TaskCompleted, start, end) }

// pePRD builds a record with one story holding tasks.
func pePRD(t *testing.T, id string, status PRDStatus, created string, tasks ...Task) *PRD {
	t.Helper()
	p := &PRD{ID: id, Title: "Add " + id, Status: status}
	if created != "" {
		p.CreatedAt = peT(t, created)
	}
	if len(tasks) > 0 {
		p.Story = []Story{{ID: id + "-s", PRDID: id, Tasks: tasks}}
	}
	return p
}

func peOpts(t *testing.T) ProgressOptions {
	return ProgressOptions{
		Since: peT(t, "2026-10-10T00:00:00Z"),
		Now:   peT(t, "2026-10-20T12:00:00Z"),
	}
}

func peNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func peNearPtr(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	peNear(t, name, *got, want)
}

func peNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Fatalf("%s = %v, want nil", name, *got)
	}
}

// peWorked is the worked example used by several tests; its numbers are worked
// out by hand in the assertions.
func peWorked(t *testing.T) []*PRD {
	a := pePRD(t, "aaaa0001", PRDCompleted, "2026-10-18T00:00:00Z",
		peDone(t, "2026-10-19T10:00:00Z", "2026-10-19T11:00:00Z"),
		peDone(t, "2026-10-19T11:00:00Z", "2026-10-19T13:00:00Z"))
	a.ApprovedAt = peP(peT(t, "2026-10-18T02:00:00Z"))
	b := pePRD(t, "bbbb0002", PRDCompleted, "2026-10-19T06:00:00Z",
		peDone(t, "2026-10-19T12:00:00Z", "2026-10-19T12:30:00Z"))
	b.ApprovedAt = peP(peT(t, "2026-10-19T07:00:00Z"))
	c := pePRD(t, "cccc0003", PRDRunning, "2026-10-19T00:00:00Z",
		peDone(t, "2026-10-20T08:00:00Z", "2026-10-20T10:00:00Z"),
		peTask(t, TaskPending, "", ""))
	c.Title = "Fix the stalled queue"
	return []*PRD{a, b, c}
}

func TestProgressEngineWorkedExample(t *testing.T) {
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})

	if m.Lanes != 5 {
		t.Fatalf("lanes = %d, want the default 5", m.Lanes)
	}
	if m.Scope != "all" {
		t.Fatalf("scope = %q", m.Scope)
	}
	// One inferred window: 13:00 on the 19th to 08:00 on the 20th, 19 hours. The
	// calendar runs from the first creation (10-18 00:00) to now: 60 hours.
	peNear(t, "calendar_days", m.CalendarDays, 2.5)
	peNear(t, "active_days", m.ActiveDays, 41.0/24.0)
	if len(m.Inactive.Windows) != 1 || !m.Inactive.Windows[0].Inferred {
		t.Fatalf("windows = %+v, want one inferred window", m.Inactive.Windows)
	}
	peNear(t, "excluded_hours", m.Inactive.ExcludedHours, 19)
	peNear(t, "share_of_calendar", m.Inactive.ShareOfCalendar, 19.0/60.0)

	f, b := m.Groups["feature"], m.Groups["bug"]
	if f.PRDs != 2 || f.Done != 2 || f.Tasks != 3 || f.TasksDone != 3 || f.TasksLeft != 0 {
		t.Fatalf("feature = %+v", f)
	}
	peNear(t, "feature work", f.WorkHours, 3.5)
	peNearPtr(t, "feature median wall", f.MedianPRDWallHours, 3) // upper middle of [0.5, 3]
	peNearPtr(t, "feature mean wall", f.MeanPRDWallHours, 1.75)
	peNearPtr(t, "feature median task", f.MedianTaskHours, 1)
	peNearPtr(t, "feature mean task", f.MeanTaskHours, 3.5/3)
	if b.PRDs != 1 || b.Running != 1 || b.Tasks != 2 || b.TasksDone != 1 || b.TasksLeft != 1 {
		t.Fatalf("bug = %+v", b)
	}
	peNear(t, "bug work", b.WorkHours, 2)
	peNil(t, "bug median wall", b.MedianPRDWallHours) // nothing completed in the group
	peNearPtr(t, "bug median task", b.MedianTaskHours, 2)

	tt := m.Totals
	if tt.PRDs != 3 || tt.Done != 2 || tt.Running != 1 || tt.Tasks != 5 || tt.TasksDone != 4 || tt.TasksLeft != 1 {
		t.Fatalf("totals = %+v", tt)
	}
	peNear(t, "total work", tt.WorkHours, 5.5)
	peNearPtr(t, "total median wall", tt.MedianPRDWallHours, 3)
	peNearPtr(t, "total median task", tt.MedianTaskHours, 2) // [0.5 1 2 2], index 2
	peNearPtr(t, "total mean task", tt.MeanTaskHours, 5.5/4)
	if m.CompletedPRDs != 2 || m.RunningPRDs != 1 {
		t.Fatalf("completed/running = %d/%d", m.CompletedPRDs, m.RunningPRDs)
	}

	th := m.Throughput
	if th.PRDsLast3d != 2 || th.PRDsLast24h != 2 || th.TasksLast24h != 3 {
		t.Fatalf("throughput counts = %+v", th)
	}
	peNear(t, "prds_per_day_3d", th.PRDsPerDay3d, 2.0/(53.0/24.0))
	peNear(t, "prds_per_day_24h", th.PRDsPerDay24h, 2.0/(5.0/24.0))
	peNear(t, "tasks_per_day", th.TasksPerDay, 4.0/(41.0/24.0))
	peNear(t, "tasks_per_day_24h", th.TasksPerDay24h, 3.0/(5.0/24.0))

	u := m.Utilization
	peNear(t, "worker hours per day", u.WorkerHoursPerDay, 5.5/(41.0/24.0))
	peNear(t, "lane capacity", u.LaneCapacityHoursPerDay, 120)
	peNear(t, "ratio", u.Ratio, u.WorkerHoursPerDay/120)

	lt := m.LeadTime
	if lt.Samples != 2 {
		t.Fatalf("lead samples = %d", lt.Samples)
	}
	peNearPtr(t, "created->approved", lt.CreatedToApprovedMedianHours, 2)
	peNearPtr(t, "approved->first task", lt.ApprovedToFirstTaskMedianHours, 32)
	peNearPtr(t, "run", lt.RunMedianHours, 3)
	peNearPtr(t, "created->last done", lt.CreatedToLastDoneMedianHours, 37)
	// Every lead-time stage has samples; only the absent backlog is reported.
	if strings.Join(m.NotMeasured, ",") != "backlog" {
		t.Fatalf("not_measured = %v, want only the missing backlog", m.NotMeasured)
	}
}

func TestProgressEngineCountsByKindAndState(t *testing.T) {
	cd := "2026-10-12T00:00:00Z"
	allDone := []Task{peDone(t, "2026-10-13T00:00:00Z", "2026-10-13T01:00:00Z")}
	cancelledAll := pePRD(t, "c0000001", PRDCancelled, cd, allDone...)
	cancelledPartial := pePRD(t, "c0000002", PRDCancelled, cd, allDone[0], peTask(t, TaskPending, "", ""))
	cancelledEmpty := pePRD(t, "c0000003", PRDCancelled, cd)
	tpl := pePRD(t, "d0000001", PRDDraft, cd)
	tpl.IsTemplate = true
	explicitOther := pePRD(t, "e0000001", PRDDraft, cd)
	explicitOther.Kind = "other"
	explicitBug := pePRD(t, "e0000002", PRDApproved, cd)
	explicitBug.Kind = "bug"
	bugByTitle := pePRD(t, "e0000003", PRDNeedsReview, cd)
	bugByTitle.Title = "Fix the crash on start"
	prds := []*PRD{
		pePRD(t, "a0000001", PRDCompleted, cd, allDone...),
		cancelledAll, cancelledPartial, cancelledEmpty,
		pePRD(t, "a0000002", PRDRejected, cd),
		pePRD(t, "a0000003", PRDArchived, cd),
		tpl,
		pePRD(t, "b0000001", PRDRunning, cd),
		pePRD(t, "b0000002", PRDPlanning, cd),
		pePRD(t, "b0000003", PRDStatus("decomposing"), cd),
		pePRD(t, "b0000004", PRDBlocked, cd),
		pePRD(t, "b0000005", PRDFailed, cd),
		pePRD(t, "b0000006", PRDDraft, cd),
		pePRD(t, "b0000007", PRDPaused, cd),
		explicitOther, explicitBug, bugByTitle,
		nil,
	}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})

	tt := m.Totals
	// Counted: a1 + cancelledAll (done 2); 3 running; blocked+failed (2); waiting:
	// draft, paused, explicitOther, explicitBug, bugByTitle (5).
	if tt.PRDs != 12 || tt.Done != 2 || tt.Running != 3 || tt.Blocked != 2 || tt.Waiting != 5 {
		t.Fatalf("totals = %+v", tt)
	}
	if tt.Tasks != 2 || tt.TasksDone != 2 {
		t.Fatalf("tasks = %d/%d, want 2/2 (cancelled-partial is not counted)", tt.TasksDone, tt.Tasks)
	}
	if got := m.Groups["other"].PRDs; got != 1 {
		t.Fatalf("other = %d", got)
	}
	if got := m.Groups["bug"].PRDs; got != 2 {
		t.Fatalf("bug = %d", got)
	}
	if got := m.Groups["feature"].PRDs; got != 9 {
		t.Fatalf("feature = %d", got)
	}
	for _, k := range []string{"feature", "bug", "other"} {
		if _, ok := m.Groups[k]; !ok {
			t.Fatalf("group %q missing", k)
		}
	}
}

func TestProgressEngineKindFromBacklogRow(t *testing.T) {
	p := pePRD(t, "f1f1f1f1", PRDDraft, "2026-10-12T00:00:00Z")
	rows := []BacklogRow{{Number: "1", Item: "Anything", Kind: "bug", PRDIDs: []string{"f1f1f1f1"}}}
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{p}, Backlog: rows, Options: peOpts(t)})
	if m.Groups["bug"].PRDs != 1 || m.Groups["feature"].PRDs != 0 {
		t.Fatalf("groups = %+v", m.Groups)
	}
	if !m.BacklogAvailable {
		t.Fatal("backlog_available = false")
	}
}

func TestProgressEngineSinceKeepsOldOpenRecords(t *testing.T) {
	old := "2026-09-01T00:00:00Z"
	prds := []*PRD{
		pePRD(t, "00000001", PRDCompleted, old, peDone(t, "2026-09-02T00:00:00Z", "2026-09-02T01:00:00Z")),
		pePRD(t, "00000002", PRDRunning, old),
		pePRD(t, "00000003", PRDDraft, ""), // no created_at counts as now
		pePRD(t, "00000004", PRDCompleted, "", peDone(t, "2026-10-15T00:00:00Z", "2026-10-15T02:00:00Z")),
	}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})
	if m.Totals.PRDs != 3 || m.Totals.Done != 1 || m.Totals.Running != 1 || m.Totals.Waiting != 1 {
		t.Fatalf("totals = %+v", m.Totals)
	}
}

func TestProgressEngineUntilDropsLaterRecordsAndCapsNow(t *testing.T) {
	o := peOpts(t)
	o.Until = peT(t, "2026-10-18T12:00:00Z")
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: o})
	if m.Totals.PRDs != 1 || m.Totals.Done != 1 {
		t.Fatalf("totals = %+v, want only the first record", m.Totals)
	}
	if !m.GeneratedAt.Equal(o.Until) {
		t.Fatalf("generated_at = %v, want until %v", m.GeneratedAt, o.Until)
	}
	peNear(t, "calendar_days", m.CalendarDays, 0.5) // 10-18 00:00 to 12:00
}

func TestProgressEngineUntilAfterNowDoesNotExtendNow(t *testing.T) {
	o := peOpts(t)
	o.Until = peT(t, "2026-12-01T00:00:00Z")
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: o})
	if !m.GeneratedAt.Equal(o.Now) {
		t.Fatalf("generated_at = %v, want %v", m.GeneratedAt, o.Now)
	}
}

func TestProgressEngineScopeFilters(t *testing.T) {
	in := func(id, auto string, tags ...string) *PRD {
		p := pePRD(t, id, PRDDraft, "2026-10-12T00:00:00Z")
		p.AutomationID = auto
		p.TagIDs = tags
		return p
	}
	prds := []*PRD{
		in("11110001", "auto-1", "t1"),
		in("11110002", "auto-2", "t1", "t2"),
		in("11110003", "", "t2"),
		in("11110004", ""),
	}
	run := func(mod func(*ProgressOptions)) ProgressMetrics {
		o := peOpts(t)
		mod(&o)
		return ComputeProgress(ProgressInput{PRDs: prds, Options: o})
	}

	m := run(func(o *ProgressOptions) { o.AutomationID = "auto-1" })
	if m.Totals.PRDs != 1 || m.Scope != "automation:auto-1" {
		t.Fatalf("automation scope: prds=%d scope=%q", m.Totals.PRDs, m.Scope)
	}
	m = run(func(o *ProgressOptions) {
		o.AutomationID = "auto-1"
		o.MemberIDs = map[string]bool{"11110003": true}
	})
	if m.Totals.PRDs != 2 {
		t.Fatalf("member filter: prds=%d, want 2", m.Totals.PRDs)
	}
	m = run(func(o *ProgressOptions) { o.TagID = "t2" })
	if m.Totals.PRDs != 2 || m.Scope != "tag:t2" {
		t.Fatalf("tag scope: prds=%d scope=%q", m.Totals.PRDs, m.Scope)
	}
	m = run(func(o *ProgressOptions) { o.AutomationID = "auto-2"; o.TagID = "t2" })
	if m.Totals.PRDs != 1 {
		t.Fatalf("both filters: prds=%d, want 1", m.Totals.PRDs)
	}
	m = run(func(o *ProgressOptions) {})
	if m.Totals.PRDs != 4 || m.Scope != "all" {
		t.Fatalf("no scope: prds=%d scope=%q", m.Totals.PRDs, m.Scope)
	}
}

func TestProgressEngineWindowsExcludedFromRates(t *testing.T) {
	base := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})

	// One explicit window that is separate (12 hours on the 18th), one that sits
	// inside the inferred 19-hour gap and so must not be counted twice.
	explicit := []InactiveWindow{
		{Start: peT(t, "2026-10-18T00:00:00Z"), End: peT(t, "2026-10-18T12:00:00Z"), Kind: "build", Reason: "waiting for a merge decision"},
		{Start: peT(t, "2026-10-19T20:00:00Z"), End: peT(t, "2026-10-20T02:00:00Z"), Kind: "design"},
	}
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Windows: explicit, Options: peOpts(t)})

	peNear(t, "calendar_days unchanged", m.CalendarDays, base.CalendarDays)
	peNear(t, "excluded_hours", m.Inactive.ExcludedHours, 31)
	peNear(t, "active_days", m.ActiveDays, 29.0/24.0)
	peNear(t, "share_of_calendar", m.Inactive.ShareOfCalendar, 31.0/60.0)
	if m.ActiveDays >= base.ActiveDays {
		t.Fatalf("active days did not shrink: %v vs %v", m.ActiveDays, base.ActiveDays)
	}
	// A shorter active period raises every per-day rate.
	if m.Throughput.TasksPerDay <= base.Throughput.TasksPerDay {
		t.Fatalf("tasks_per_day %v not above %v", m.Throughput.TasksPerDay, base.Throughput.TasksPerDay)
	}
	if m.Utilization.WorkerHoursPerDay <= base.Utilization.WorkerHoursPerDay {
		t.Fatalf("worker hours per day did not rise")
	}
	peNear(t, "tasks_per_day", m.Throughput.TasksPerDay, 4.0/(29.0/24.0))
	if len(m.Inactive.Windows) != 2 {
		t.Fatalf("windows = %+v, want two after merging", m.Inactive.Windows)
	}
}

func TestProgressEngineGapHoursSetsInference(t *testing.T) {
	o := peOpts(t)
	o.GapHours = 30 // longer than any gap in the example
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: o})
	if len(m.Inactive.Windows) != 0 {
		t.Fatalf("windows = %+v, want none at a 30 hour gap", m.Inactive.Windows)
	}
	peNear(t, "active_days", m.ActiveDays, 2.5)
}

func TestProgressEngineLanesOverride(t *testing.T) {
	o := peOpts(t)
	o.Lanes = 2
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: o})
	if m.Lanes != 2 {
		t.Fatalf("lanes = %d", m.Lanes)
	}
	peNear(t, "lane capacity", m.Utilization.LaneCapacityHoursPerDay, 48)
}

func TestProgressEngineNullNotZeroWhenUnmeasured(t *testing.T) {
	// Only a draft record: nothing was timed.
	prds := []*PRD{pePRD(t, "22220001", PRDDraft, "2026-10-19T00:00:00Z")}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})

	g := m.Totals
	peNil(t, "median wall", g.MedianPRDWallHours)
	peNil(t, "mean wall", g.MeanPRDWallHours)
	peNil(t, "mean task", g.MeanTaskHours)
	peNil(t, "median task", g.MedianTaskHours)
	lt := m.LeadTime
	peNil(t, "created->approved", lt.CreatedToApprovedMedianHours)
	peNil(t, "approved->first", lt.ApprovedToFirstTaskMedianHours)
	peNil(t, "run", lt.RunMedianHours)
	peNil(t, "lead", lt.CreatedToLastDoneMedianHours)
	if lt.Samples != 0 {
		t.Fatalf("samples = %d", lt.Samples)
	}
	want := []string{
		"created_to_approved_median_hours",
		"approved_to_first_task_median_hours",
		"run_median_hours",
		"created_to_last_done_median_hours",
		"backlog",
		"tasks_per_prd",
	}
	if strings.Join(m.NotMeasured, ",") != strings.Join(want, ",") {
		t.Fatalf("not_measured = %v, want %v", m.NotMeasured, want)
	}

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, key := range []string{
		`"median_prd_wall_hours":null`, `"mean_prd_wall_hours":null`,
		`"mean_task_hours":null`, `"median_task_hours":null`,
		`"created_to_approved_median_hours":null`, `"approved_to_first_task_median_hours":null`,
		`"run_median_hours":null`, `"created_to_last_done_median_hours":null`,
	} {
		if !strings.Contains(s, key) {
			t.Fatalf("json lacks %s:\n%s", key, s)
		}
	}
	// The empty slices serialize as arrays, not null.
	for _, key := range []string{`"unfiled_rows":[]`, `"windows":[]`} {
		if !strings.Contains(s, key) {
			t.Fatalf("json lacks %s:\n%s", key, s)
		}
	}
}

func TestProgressEngineEmptyInputIsSafe(t *testing.T) {
	m := ComputeProgress(ProgressInput{Options: peOpts(t)})
	if m.Totals.PRDs != 0 || len(m.Groups) != 3 {
		t.Fatalf("totals=%+v groups=%d", m.Totals, len(m.Groups))
	}
	if math.IsNaN(m.ActiveDays) || m.ActiveDays <= 0 {
		t.Fatalf("active_days = %v", m.ActiveDays)
	}
	peNear(t, "calendar floor", m.CalendarDays, 0.1)
	for _, v := range []float64{m.Throughput.TasksPerDay, m.Throughput.TasksPerDay24h, m.Utilization.Ratio, m.Inactive.ShareOfCalendar} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("non-finite value %v", v)
		}
	}
	if _, err := json.Marshal(m); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestProgressEngineJSONUsesSnakeCaseTags(t *testing.T) {
	m := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"generated_at", "since", "scope", "calendar_days", "active_days", "lanes", "groups", "totals",
		"completed_prds", "running_prds", "throughput", "utilization", "lead_time", "inactive",
		"remaining", "completion", "unfiled_rows", "backlog_available", "not_measured",
	} {
		if _, ok := top[k]; !ok {
			t.Fatalf("json lacks top-level key %q", k)
		}
	}
	if strings.Contains(string(raw), `"PRDs"`) || strings.Contains(string(raw), `"Done"`) {
		t.Fatalf("json holds a Go field name: %s", raw)
	}
}

// Lesson: measuring record time from approval to the last update overstated it
// by an order of magnitude. The time is first task start to last task completion.
func TestProgressEngineRecordTimeIsFirstStartToLastCompletion(t *testing.T) {
	p := pePRD(t, "33330001", PRDCompleted, "2026-10-11T00:00:00Z",
		peDone(t, "2026-10-19T10:00:00Z", "2026-10-19T10:30:00Z"),
		peDone(t, "2026-10-19T10:30:00Z", "2026-10-19T12:00:00Z"))
	p.ApprovedAt = peP(peT(t, "2026-10-11T01:00:00Z"))
	p.UpdatedAt = peT(t, "2026-10-20T11:00:00Z") // a later edit, days after approval
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{p}, Options: peOpts(t)})
	peNearPtr(t, "median wall", m.Totals.MedianPRDWallHours, 2)
	peNearPtr(t, "mean wall", m.Totals.MeanPRDWallHours, 2)
	approvalToUpdate := Hours(*p.ApprovedAt, p.UpdatedAt)
	if *m.Totals.MedianPRDWallHours >= approvalToUpdate/10 {
		t.Fatalf("wall time %v is not far below approval-to-update %v", *m.Totals.MedianPRDWallHours, approvalToUpdate)
	}
}

// Lesson: counting records touched in the last 24 hours as throughput counted
// the cleanup actions. A record completed when its last task completed.
func TestProgressEngineThroughputCountsLastTaskCompletionNotUpdateTime(t *testing.T) {
	now := peOpts(t).Now
	oldDone := pePRD(t, "44440001", PRDCompleted, "2026-10-11T00:00:00Z",
		peDone(t, "2026-10-12T00:00:00Z", "2026-10-12T01:00:00Z"))
	oldDone.UpdatedAt = now.Add(-time.Minute) // a cleanup touched it just now
	freshDone := pePRD(t, "44440002", PRDCompleted, "2026-10-11T00:00:00Z",
		peDone(t, "2026-10-20T09:00:00Z", "2026-10-20T11:00:00Z"))
	freshDone.UpdatedAt = peT(t, "2026-10-12T00:00:00Z")
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{oldDone, freshDone}, Options: peOpts(t)})
	if m.Throughput.PRDsLast24h != 1 || m.Throughput.PRDsLast3d != 1 {
		t.Fatalf("throughput = %+v, want exactly the freshly finished record", m.Throughput)
	}
	if m.CompletedPRDs != 2 {
		t.Fatalf("completed = %d", m.CompletedPRDs)
	}
}

func TestProgressEngineThroughputWindowEdges(t *testing.T) {
	now := peOpts(t).Now
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	mk := func(id string, ago time.Duration) *PRD {
		return pePRD(t, id, PRDCompleted, "2026-10-11T00:00:00Z", peDone(t, at(ago+time.Hour), at(ago)))
	}
	prds := []*PRD{
		mk("55550001", 24*time.Hour),             // exactly 24 hours before now: inside
		mk("55550002", 24*time.Hour+time.Minute), // just outside 24h, inside 3d
		mk("55550003", 72*time.Hour),             // exactly 3 days: inside
		mk("55550004", 72*time.Hour+time.Minute), // outside both
	}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})
	if m.Throughput.PRDsLast24h != 1 || m.Throughput.PRDsLast3d != 3 {
		t.Fatalf("throughput = %+v, want 1 in 24h and 3 in 3d", m.Throughput)
	}
}

func TestProgressEngineTasksLast24hUsesCompletionTime(t *testing.T) {
	prds := []*PRD{pePRD(t, "66660001", PRDRunning, "2026-10-11T00:00:00Z",
		peDone(t, "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z"), // outside
		peDone(t, "2026-10-19T11:00:00Z", "2026-10-19T12:00:00Z"), // exactly 24h: inside
		peDone(t, "2026-10-20T00:00:00Z", "2026-10-20T01:00:00Z"), // inside
		peTask(t, TaskInProgress, "2026-10-20T11:00:00Z", ""))}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})
	if m.Throughput.TasksLast24h != 2 || m.Totals.TasksDone != 3 {
		t.Fatalf("tasks last 24h = %d, done = %d", m.Throughput.TasksLast24h, m.Totals.TasksDone)
	}
}

func TestProgressEngineWorkHoursNeedPositiveSpan(t *testing.T) {
	prds := []*PRD{pePRD(t, "77770001", PRDRunning, "2026-10-11T00:00:00Z",
		peDone(t, "2026-10-19T10:00:00Z", "2026-10-19T10:00:00Z"), // zero span: no work time
		peDone(t, "2026-10-19T12:00:00Z", "2026-10-19T11:00:00Z"), // negative span
		peDone(t, "", "2026-10-19T11:00:00Z"),                     // no start
		peDone(t, "2026-10-19T09:00:00Z", "2026-10-19T09:30:00Z"))}
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})
	peNear(t, "work", m.Totals.WorkHours, 0.5)
	peNearPtr(t, "mean task", m.Totals.MeanTaskHours, 0.5)
	if m.Totals.TasksDone != 4 {
		t.Fatalf("tasks done = %d", m.Totals.TasksDone)
	}
}

func TestProgressEngineLeadTimeStageWithoutSamplesIsNull(t *testing.T) {
	// No approval time: the two approval stages have no samples; the run and the
	// end-to-end stages do.
	p := pePRD(t, "88880001", PRDCompleted, "2026-10-18T00:00:00Z",
		peDone(t, "2026-10-19T00:00:00Z", "2026-10-19T04:00:00Z"))
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{p}, Options: peOpts(t)})
	lt := m.LeadTime
	peNil(t, "created->approved", lt.CreatedToApprovedMedianHours)
	peNil(t, "approved->first", lt.ApprovedToFirstTaskMedianHours)
	peNearPtr(t, "run", lt.RunMedianHours, 4)
	peNearPtr(t, "lead", lt.CreatedToLastDoneMedianHours, 28)
	want := "created_to_approved_median_hours,approved_to_first_task_median_hours,backlog"
	if strings.Join(m.NotMeasured, ",") != want {
		t.Fatalf("not_measured = %v", m.NotMeasured)
	}
	if lt.Samples != 1 {
		t.Fatalf("samples = %d", lt.Samples)
	}
}

func TestProgressEngineLeadTimeSkipsOutOfOrderStages(t *testing.T) {
	// Approved before it was created, and the first task started before approval:
	// those two stages are skipped for this record, not reported as negative.
	p := pePRD(t, "99990001", PRDCompleted, "2026-10-18T00:00:00Z",
		peDone(t, "2026-10-18T05:00:00Z", "2026-10-18T06:00:00Z"))
	p.ApprovedAt = peP(peT(t, "2026-10-17T00:00:00Z"))
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{p}, Options: peOpts(t)})
	peNil(t, "created->approved", m.LeadTime.CreatedToApprovedMedianHours)
	peNearPtr(t, "approved->first", m.LeadTime.ApprovedToFirstTaskMedianHours, 29)
	peNearPtr(t, "lead", m.LeadTime.CreatedToLastDoneMedianHours, 6)
}

func TestProgressEngineLeadTimeIgnoresOpenAndUntimedRecords(t *testing.T) {
	running := pePRD(t, "aaaa1001", PRDRunning, "2026-10-18T00:00:00Z",
		peDone(t, "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z"))
	noStart := pePRD(t, "aaaa1002", PRDCompleted, "2026-10-18T00:00:00Z",
		peDone(t, "", "2026-10-19T01:00:00Z"))
	noCreated := pePRD(t, "aaaa1003", PRDCompleted, "",
		peDone(t, "2026-10-19T00:00:00Z", "2026-10-19T01:00:00Z"))
	m := ComputeProgress(ProgressInput{PRDs: []*PRD{running, noStart, noCreated}, Options: peOpts(t)})
	if m.LeadTime.Samples != 0 {
		t.Fatalf("samples = %d, want 0", m.LeadTime.Samples)
	}
}

func TestProgressEngineUnfiledRowsAndBacklogFlag(t *testing.T) {
	rows := []BacklogRow{
		{Number: "1", Item: "Filed", Open: true, PRDIDs: []string{"abcdef01"}},
		{Number: "2", Item: "Not filed", Open: true},
		{Number: "3", Item: "Closed", Open: false},
	}
	m := ComputeProgress(ProgressInput{Backlog: rows, Options: peOpts(t)})
	if !m.BacklogAvailable || len(m.UnfiledRows) != 1 || m.UnfiledRows[0].Number != "2" {
		t.Fatalf("backlog=%v unfiled=%+v", m.BacklogAvailable, m.UnfiledRows)
	}
	m = ComputeProgress(ProgressInput{Options: peOpts(t)})
	if m.BacklogAvailable {
		t.Fatal("backlog_available = true with no rows")
	}
}

type peFakeSource struct {
	prds  []*PRD
	calls int
}

func (f *peFakeSource) ListPRDs() []*PRD {
	f.calls++
	return f.prds
}

func TestProgressEngineCollectUsesSourceWithoutAStore(t *testing.T) {
	src := &peFakeSource{prds: peWorked(t)}
	got := CollectProgress(src, nil, nil, peOpts(t))
	want := ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)})
	if src.calls != 1 {
		t.Fatalf("ListPRDs called %d times, want 1", src.calls)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("collect differs from compute:\n%s\n%s", a, b)
	}
	// A nil source is an empty list, not a panic.
	if m := CollectProgress(nil, nil, nil, peOpts(t)); m.Totals.PRDs != 0 {
		t.Fatalf("nil source: %+v", m.Totals)
	}
}

func TestProgressEngineDeterministicForFixedNow(t *testing.T) {
	rev := peWorked(t)
	rev[0], rev[2] = rev[2], rev[0]
	first, _ := json.Marshal(ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)}))
	for i := 0; i < 5; i++ {
		again, _ := json.Marshal(ComputeProgress(ProgressInput{PRDs: peWorked(t), Options: peOpts(t)}))
		if string(first) != string(again) {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, again)
		}
	}
	reordered, _ := json.Marshal(ComputeProgress(ProgressInput{PRDs: rev, Options: peOpts(t)}))
	if string(first) != string(reordered) {
		t.Fatalf("input order changed the result:\n%s\n%s", first, reordered)
	}
}

func TestProgressEngineDoesNotChangeItsInput(t *testing.T) {
	cancelled := pePRD(t, "bbbb2001", PRDCancelled, "2026-10-12T00:00:00Z",
		peDone(t, "2026-10-13T00:00:00Z", "2026-10-13T01:00:00Z"))
	windows := []InactiveWindow{
		{Start: peT(t, "2026-10-15T00:00:00Z"), End: peT(t, "2026-10-15T06:00:00Z"), Kind: "design"},
		{Start: peT(t, "2026-10-14T00:00:00Z"), End: peT(t, "2026-10-14T06:00:00Z"), Kind: "build"},
	}
	ComputeProgress(ProgressInput{PRDs: []*PRD{cancelled}, Windows: windows, Options: peOpts(t)})
	if cancelled.Status != PRDCancelled {
		t.Fatalf("status was rewritten to %q", cancelled.Status)
	}
	if windows[0].Kind != "design" || windows[1].Kind != "build" {
		t.Fatalf("windows were reordered: %+v", windows)
	}
}
