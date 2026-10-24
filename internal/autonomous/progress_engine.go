package autonomous

import (
	"time"
)

// The progress engine turns the Automata list (plus the backlog rows and the
// inactive windows) into the metrics the dashboard, CLI, REST and MCP show. It
// is pure computation: no file, network or clock access except time.Now when
// ProgressOptions.Now is left zero. scripts/v10-status.py is the reference
// oracle; every rule below is a port of its main().

const (
	defaultProgressLanes    = 5
	defaultProgressGapHours = 3.0
	progressThreeDays       = 72 * time.Hour
	progressOneDay          = 24 * time.Hour
	progressMinCalendarDays = 0.1
)

// Names of the lead-time stages, used in ProgressMetrics.NotMeasured when a
// stage has no samples.
const (
	leadStageCreatedToApproved  = "created_to_approved_median_hours"
	leadStageApprovedToFirst    = "approved_to_first_task_median_hours"
	leadStageRun                = "run_median_hours"
	leadStageCreatedToLastDone  = "created_to_last_done_median_hours"
	progressScopeAll            = "all"
	progressScopeAutomationPart = "automation:"
	progressScopeTagPart        = "tag:"
)

// ProgressOptions tunes one computation.
type ProgressOptions struct {
	Since        time.Time       `json:"since"`
	Until        time.Time       `json:"until"`     // zero = now
	Now          time.Time       `json:"now"`       // zero = the wall clock
	Lanes        int             `json:"lanes"`     // 0 means 5
	GapHours     float64         `json:"gap_hours"` // 0 means 3
	AutomationID string          `json:"automation_id"`
	TagID        string          `json:"tag_id"`
	MemberIDs    map[string]bool `json:"member_ids"` // explicit member filter for an Automation
}

// ProgressInput is everything ComputeProgress reads.
type ProgressInput struct {
	PRDs    []*PRD           `json:"prds"`
	Backlog []BacklogRow     `json:"backlog"`
	Windows []InactiveWindow `json:"windows"` // explicit windows from the windows file
	Options ProgressOptions  `json:"options"`
}

// ProgressGroup is the count block for one kind of Automaton, or for all of
// them. The mean and median fields are nil when nothing could be measured.
type ProgressGroup struct {
	PRDs               int      `json:"prds"`
	Done               int      `json:"done"`
	Running            int      `json:"running"`
	Waiting            int      `json:"waiting"`
	Blocked            int      `json:"blocked"`
	Tasks              int      `json:"tasks"`
	TasksDone          int      `json:"tasks_done"`
	TasksLeft          int      `json:"tasks_left"`
	WorkHours          float64  `json:"work_hours"`
	MedianPRDWallHours *float64 `json:"median_prd_wall_hours"`
	MeanPRDWallHours   *float64 `json:"mean_prd_wall_hours"`
	MeanTaskHours      *float64 `json:"mean_task_hours"`
	MedianTaskHours    *float64 `json:"median_task_hours"`
}

// ProgressThroughput is the completion rate block. Rates are per ACTIVE day.
type ProgressThroughput struct {
	PRDsPerDay3d   float64 `json:"prds_per_day_3d"`
	PRDsPerDay24h  float64 `json:"prds_per_day_24h"`
	PRDsLast3d     int     `json:"prds_last_3d"`
	PRDsLast24h    int     `json:"prds_last_24h"`
	TasksPerDay    float64 `json:"tasks_per_day"`
	TasksLast24h   int     `json:"tasks_last_24h"`
	TasksPerDay24h float64 `json:"tasks_per_day_24h"`
}

// ProgressUtilization compares the worker hours done per active day with what
// the lanes could do.
type ProgressUtilization struct {
	WorkerHoursPerDay       float64 `json:"worker_hours_per_day"`
	LaneCapacityHoursPerDay float64 `json:"lane_capacity_hours_per_day"`
	Ratio                   float64 `json:"ratio"`
}

// ProgressLeadTime holds the median of each stage between creating an
// Automaton and finishing it. It is tracked state only; no estimate uses it. A
// stage with no samples is nil.
type ProgressLeadTime struct {
	CreatedToApprovedMedianHours   *float64 `json:"created_to_approved_median_hours"`
	ApprovedToFirstTaskMedianHours *float64 `json:"approved_to_first_task_median_hours"`
	RunMedianHours                 *float64 `json:"run_median_hours"`
	CreatedToLastDoneMedianHours   *float64 `json:"created_to_last_done_median_hours"`
	Samples                        int      `json:"samples"`
}

// ProgressInactive reports the stretches excluded from every rate.
type ProgressInactive struct {
	Windows              []InactiveWindow `json:"windows"`
	ExcludedHours        float64          `json:"excluded_hours"`
	ShareOfCalendar      float64          `json:"share_of_calendar"`
	CalendarEstimateDays *float64         `json:"calendar_estimate_days"`
}

// TasksPerPRD describes how many tasks the decomposed records hold.
type TasksPerPRD struct {
	Mean    float64 `json:"mean"`
	Median  float64 `json:"median"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Samples int     `json:"samples"`
}

// RemainingKnown is the work that already exists as records.
type RemainingKnown struct {
	PRDs             int `json:"prds"`
	Tasks            int `json:"tasks"`
	UndecomposedPRDs int `json:"undecomposed_prds"`
}

// RemainingEstimated is the work that has to be estimated: records not filed
// yet and records with no tasks yet. It is always labelled as an estimate.
type RemainingEstimated struct {
	NewPRDs          int                    `json:"new_prds"`
	UndecomposedPRDs int                    `json:"undecomposed_prds"`
	EstimatedTasks   float64                `json:"estimated_tasks"`
	TasksPerPRD      TasksPerPRD            `json:"tasks_per_prd"`
	PerKind          map[string]TasksPerPRD `json:"per_kind"`
}

// RemainingCombined is the known and estimated work added together.
type RemainingCombined struct {
	PRDs  int     `json:"prds"`
	Tasks float64 `json:"tasks"`
	Label string  `json:"label"`
}

// RemainingWork is what is left to do before the target. It is filled by
// computeRemaining.
type RemainingWork struct {
	Known     RemainingKnown     `json:"known"`
	Estimated RemainingEstimated `json:"estimated"`
	Combined  RemainingCombined  `json:"combined"`
}

// CompletionRange is the estimated time to finish, as a range with the method
// behind each figure. It is filled by computeRemaining.
type CompletionRange struct {
	ByPRDRateDays     *float64          `json:"by_prd_rate_days"`
	ByTaskRateDays    *float64          `json:"by_task_rate_days"`
	ByTaskRate24hDays *float64          `json:"by_task_rate_24h_days"`
	ByWorkerHoursDays *float64          `json:"by_worker_hours_days"`
	LowDays           *float64          `json:"low_days"`
	HighDays          *float64          `json:"high_days"`
	Drivers           []string          `json:"drivers"`
	Method            map[string]string `json:"method"`
	CalendarDays      *float64          `json:"calendar_days"`
}

// ProgressMetrics is the result of one computation.
type ProgressMetrics struct {
	GeneratedAt      time.Time                `json:"generated_at"`
	Since            time.Time                `json:"since"`
	Scope            string                   `json:"scope"`
	CalendarDays     float64                  `json:"calendar_days"`
	ActiveDays       float64                  `json:"active_days"`
	Lanes            int                      `json:"lanes"`
	Groups           map[string]ProgressGroup `json:"groups"` // feature, bug and other, always all three
	Totals           ProgressGroup            `json:"totals"`
	CompletedPRDs    int                      `json:"completed_prds"`
	RunningPRDs      int                      `json:"running_prds"`
	Throughput       ProgressThroughput       `json:"throughput"`
	Utilization      ProgressUtilization      `json:"utilization"`
	LeadTime         ProgressLeadTime         `json:"lead_time"`
	Inactive         ProgressInactive         `json:"inactive"`
	Remaining        RemainingWork            `json:"remaining"`
	Completion       CompletionRange          `json:"completion"`
	UnfiledRows      []BacklogRow             `json:"unfiled_rows"`
	BacklogAvailable bool                     `json:"backlog_available"`
	NotMeasured      []string                 `json:"not_measured"`
}

// ProgressSource supplies the Automata the engine counts.
type ProgressSource interface {
	ListPRDs() []*PRD
}

// CollectProgress reads the records from src and computes the metrics. The
// source is the only place records come from, so tests pass a fake.
func CollectProgress(src ProgressSource, backlog []BacklogRow, windows []InactiveWindow, opts ProgressOptions) ProgressMetrics {
	var prds []*PRD
	if src != nil {
		prds = src.ListPRDs()
	}
	return ComputeProgress(ProgressInput{PRDs: prds, Backlog: backlog, Windows: windows, Options: opts})
}

// progressRec is one record that passed the filters, with the status and kind
// the engine counts it under.
type progressRec struct {
	prd    *PRD
	status PRDStatus
	kind   string
}

// progressAcc accumulates one group's numbers.
type progressAcc struct {
	g         ProgressGroup
	walls     []float64
	taskHours []float64
}

func (a *progressAcc) add(o *progressAcc) {
	a.g.PRDs += o.g.PRDs
	a.g.Done += o.g.Done
	a.g.Running += o.g.Running
	a.g.Waiting += o.g.Waiting
	a.g.Blocked += o.g.Blocked
	a.g.Tasks += o.g.Tasks
	a.g.TasksDone += o.g.TasksDone
	a.g.WorkHours += o.g.WorkHours
	a.walls = append(a.walls, o.walls...)
	a.taskHours = append(a.taskHours, o.taskHours...)
}

func (a *progressAcc) finish() ProgressGroup {
	g := a.g
	g.TasksLeft = g.Tasks - g.TasksDone
	g.MedianPRDWallHours = medianPtr(a.walls)
	g.MeanPRDWallHours = meanPtr(a.walls)
	g.MeanTaskHours = meanPtr(a.taskHours)
	g.MedianTaskHours = medianPtr(a.taskHours)
	return g
}

func medianPtr(xs []float64) *float64 {
	m, ok := Median(xs)
	if !ok {
		return nil
	}
	return &m
}

func meanPtr(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	m := sum / float64(len(xs))
	return &m
}

func normalizeProgressOptions(o ProgressOptions) ProgressOptions {
	if o.Now.IsZero() {
		o.Now = time.Now().UTC()
	}
	if !o.Until.IsZero() && o.Until.Before(o.Now) {
		o.Now = o.Until
	}
	if o.Lanes <= 0 {
		o.Lanes = defaultProgressLanes
	}
	if o.GapHours <= 0 {
		o.GapHours = defaultProgressGapHours
	}
	return o
}

func progressScope(o ProgressOptions) string {
	scope := ""
	if o.AutomationID != "" {
		scope = progressScopeAutomationPart + o.AutomationID
	}
	if o.TagID != "" {
		if scope != "" {
			scope += "+"
		}
		scope += progressScopeTagPart + o.TagID
	}
	if scope == "" {
		return progressScopeAll
	}
	return scope
}

// progressTasks returns every task of every story of p.
func progressTasks(p *PRD) []*Task {
	var out []*Task
	for si := range p.Story {
		for ti := range p.Story[si].Tasks {
			out = append(out, &p.Story[si].Tasks[ti])
		}
	}
	return out
}

// selectProgressRecords applies the filters of the status script: templates,
// replaced or abandoned records, the creation window and the scope.
func selectProgressRecords(prds []*PRD, o ProgressOptions, rowKinds map[string]string) []progressRec {
	var recs []progressRec
	for _, p := range prds {
		if p == nil || p.IsTemplate {
			continue
		}
		status := NormalizePRDStatus(p.Status)
		switch status {
		case PRDCancelled, PRDRejected, PRDArchived:
			// A cancelled record whose tasks all completed is merged work whose
			// record was closed at merge time: it counts as completed. Any other
			// cancelled, rejected or archived record is replaced or abandoned.
			if status == PRDCancelled && allTasksCompleted(p) {
				status = PRDCompleted
			} else {
				continue
			}
		}
		created := p.CreatedAt
		if created.IsZero() {
			created = o.Now
		}
		if !o.Until.IsZero() && created.After(o.Until) {
			continue
		}
		if created.Before(o.Since) && status == PRDCompleted {
			continue
		}
		if !inProgressScope(p, o) {
			continue
		}
		recs = append(recs, progressRec{prd: p, status: status, kind: EffectiveKind(p, rowKinds)})
	}
	return recs
}

func allTasksCompleted(p *PRD) bool {
	ts := progressTasks(p)
	if len(ts) == 0 {
		return false
	}
	for _, t := range ts {
		if t.Status != TaskCompleted {
			return false
		}
	}
	return true
}

func inProgressScope(p *PRD, o ProgressOptions) bool {
	if o.AutomationID != "" || len(o.MemberIDs) > 0 {
		if !(o.AutomationID != "" && p.AutomationID == o.AutomationID) && !o.MemberIDs[p.ID] {
			return false
		}
	}
	if o.TagID != "" {
		found := false
		for _, id := range p.TagIDs {
			if id == o.TagID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// lastTaskDone is the latest completed_at over all tasks of p.
func lastTaskDone(p *PRD) (time.Time, bool) {
	var last time.Time
	found := false
	for _, t := range progressTasks(p) {
		if t.CompletedAt != nil && (!found || t.CompletedAt.After(last)) {
			last, found = *t.CompletedAt, true
		}
	}
	return last, found
}

// firstTaskStart is the earliest started_at over all tasks of p.
func firstTaskStart(p *PRD) (time.Time, bool) {
	var first time.Time
	found := false
	for _, t := range progressTasks(p) {
		if t.StartedAt != nil && (!found || t.StartedAt.Before(first)) {
			first, found = *t.StartedAt, true
		}
	}
	return first, found
}

// ComputeProgress computes the metrics. It reads only its input.
func ComputeProgress(in ProgressInput) ProgressMetrics {
	o := normalizeProgressOptions(in.Options)
	recs := selectProgressRecords(in.PRDs, o, RowKinds(in.Backlog))

	accs := map[string]*progressAcc{
		PRDKindFeature: {},
		PRDKindBug:     {},
		PRDKindOther:   {},
	}
	var events []time.Time
	var first time.Time
	haveFirst := false
	doneTasks, doneTasksLast24h := 0, 0
	cut24 := o.Now.Add(-progressOneDay)

	for _, r := range recs {
		p := r.prd
		acc := accs[r.kind]
		if acc == nil {
			acc = accs[PRDKindOther]
		}
		acc.g.PRDs++
		switch {
		case r.status == PRDCompleted:
			acc.g.Done++
		case r.status == PRDRunning || r.status == PRDPlanning:
			acc.g.Running++
		case r.status == PRDBlocked || r.status == PRDFailed:
			acc.g.Blocked++
		default:
			acc.g.Waiting++
		}
		if !p.CreatedAt.IsZero() && (!haveFirst || p.CreatedAt.Before(first)) {
			first, haveFirst = p.CreatedAt, true
		}
		for _, t := range progressTasks(p) {
			acc.g.Tasks++
			if t.StartedAt != nil {
				events = append(events, *t.StartedAt)
			}
			if t.CompletedAt != nil {
				events = append(events, *t.CompletedAt)
			}
			if t.Status != TaskCompleted {
				continue
			}
			acc.g.TasksDone++
			doneTasks++
			// The status script counts a completed task with no completion time
			// as recent, because a missing time reads as "now".
			if t.CompletedAt == nil || !t.CompletedAt.Before(cut24) {
				doneTasksLast24h++
			}
			if t.StartedAt != nil && t.CompletedAt != nil && t.CompletedAt.After(*t.StartedAt) {
				h := Hours(*t.StartedAt, *t.CompletedAt)
				acc.g.WorkHours += h
				acc.taskHours = append(acc.taskHours, h)
			}
		}
		if r.status == PRDCompleted {
			// Time the record was actually being worked: first task start to
			// last task completion. Approval to last update is wrong: it holds
			// weeks of waiting and every later edit.
			s, okS := firstTaskStart(p)
			e, okE := lastTaskDone(p)
			if okS && okE && e.After(s) {
				acc.walls = append(acc.walls, Hours(s, e))
			}
		}
	}

	total := &progressAcc{}
	groups := make(map[string]ProgressGroup, len(accs))
	for _, k := range []string{PRDKindFeature, PRDKindBug, PRDKindOther} {
		total.add(accs[k])
		groups[k] = accs[k].finish()
	}
	totals := total.finish()

	if !haveFirst {
		first = o.Now
	}
	start := first
	if o.Since.After(start) {
		start = o.Since
	}
	calDays := Hours(start, o.Now) / 24.0
	if calDays < progressMinCalendarDays {
		calDays = progressMinCalendarDays
	}

	gap := time.Duration(o.GapHours * float64(time.Hour))
	all := append(InferInactiveWindows(events, o.Since, gap), in.Windows...)
	windows := MergeWindows(all)
	if windows == nil {
		windows = []InactiveWindow{}
	}
	activeDays := ActiveHours(start, o.Now, windows) / 24.0
	excluded := ExcludedHours(windows)
	share := 0.0
	if calDays > 0 {
		share = excluded / (calDays * 24.0)
	}

	m := ProgressMetrics{
		GeneratedAt:      o.Now,
		Since:            o.Since,
		Scope:            progressScope(o),
		CalendarDays:     calDays,
		ActiveDays:       activeDays,
		Lanes:            o.Lanes,
		Groups:           groups,
		Totals:           totals,
		CompletedPRDs:    totals.Done,
		RunningPRDs:      totals.Running,
		Inactive:         ProgressInactive{Windows: windows, ExcludedHours: excluded, ShareOfCalendar: share},
		UnfiledRows:      UnfiledRows(in.Backlog),
		BacklogAvailable: len(in.Backlog) > 0,
		NotMeasured:      []string{},
	}
	if m.UnfiledRows == nil {
		m.UnfiledRows = []BacklogRow{}
	}

	// Throughput. A completed record finished when its LAST task completed;
	// the record's own update time also moves on every later edit or cancel.
	cut3d := o.Now.Add(-progressThreeDays)
	for _, r := range recs {
		if r.status != PRDCompleted {
			continue
		}
		last, ok := lastTaskDone(r.prd)
		if !ok || last.After(o.Now) {
			continue
		}
		if !last.Before(cut3d) {
			m.Throughput.PRDsLast3d++
		}
		if !last.Before(cut24) {
			m.Throughput.PRDsLast24h++
		}
	}
	if m.Throughput.PRDsLast3d > 0 {
		m.Throughput.PRDsPerDay3d = float64(m.Throughput.PRDsLast3d) / (ActiveHours(cut3d, o.Now, windows) / 24.0)
	}
	if m.Throughput.PRDsLast24h > 0 {
		m.Throughput.PRDsPerDay24h = float64(m.Throughput.PRDsLast24h) / (ActiveHours(cut24, o.Now, windows) / 24.0)
	}
	m.Throughput.TasksPerDay = float64(doneTasks) / activeDays
	m.Throughput.TasksLast24h = doneTasksLast24h
	m.Throughput.TasksPerDay24h = float64(doneTasksLast24h) / (ActiveHours(cut24, o.Now, windows) / 24.0)

	m.Utilization.WorkerHoursPerDay = totals.WorkHours / activeDays
	m.Utilization.LaneCapacityHoursPerDay = float64(o.Lanes) * 24.0
	m.Utilization.Ratio = m.Utilization.WorkerHoursPerDay / m.Utilization.LaneCapacityHoursPerDay

	m.LeadTime, m.NotMeasured = computeLeadTime(recs)

	computeRemaining(&m, in, recs)
	return m
}

// computeLeadTime reports the median of each stage for completed records that
// have a creation time, at least one started task and a last completion. A
// stage with no samples is nil and named in the second result.
func computeLeadTime(recs []progressRec) (ProgressLeadTime, []string) {
	var appr, queue, run, lead []float64
	lt := ProgressLeadTime{}
	for _, r := range recs {
		if r.status != PRDCompleted {
			continue
		}
		p := r.prd
		f, okF := firstTaskStart(p)
		ld, okL := lastTaskDone(p)
		if p.CreatedAt.IsZero() || !okF || !okL {
			continue
		}
		lt.Samples++
		c := p.CreatedAt
		if p.ApprovedAt != nil {
			a := *p.ApprovedAt
			if !a.Before(c) {
				appr = append(appr, Hours(c, a))
			}
			if !f.Before(a) {
				queue = append(queue, Hours(a, f))
			}
		}
		if !ld.Before(f) {
			run = append(run, Hours(f, ld))
		}
		if !ld.Before(c) {
			lead = append(lead, Hours(c, ld))
		}
	}
	lt.CreatedToApprovedMedianHours = medianPtr(appr)
	lt.ApprovedToFirstTaskMedianHours = medianPtr(queue)
	lt.RunMedianHours = medianPtr(run)
	lt.CreatedToLastDoneMedianHours = medianPtr(lead)
	missing := []string{}
	for _, s := range []struct {
		name string
		v    *float64
	}{
		{leadStageCreatedToApproved, lt.CreatedToApprovedMedianHours},
		{leadStageApprovedToFirst, lt.ApprovedToFirstTaskMedianHours},
		{leadStageRun, lt.RunMedianHours},
		{leadStageCreatedToLastDone, lt.CreatedToLastDoneMedianHours},
	} {
		if s.v == nil {
			missing = append(missing, s.name)
		}
	}
	return lt, missing
}

// computeRemaining fills Remaining and Completion. It is a placeholder here:
// the remaining-work and completion-range code replaces it.
func computeRemaining(m *ProgressMetrics, in ProgressInput, recs []progressRec) {}
