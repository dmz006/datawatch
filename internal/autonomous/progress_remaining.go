package autonomous

import (
	"math"
	"sort"
)

// Names added to ProgressMetrics.NotMeasured by the remaining-work step.
const (
	notMeasuredBacklog     = "backlog"
	notMeasuredTasksPerPRD = "tasks_per_prd"
)

// Keys of CompletionRange.Method.
const (
	methodPRDRate      = "prd_rate"
	methodTaskRate     = "task_rate"
	methodTaskRate24h  = "task_rate_24h"
	methodWorkerHours  = "worker_hours"
	combinedLabel      = "known_plus_estimated"
	maxCalendarShare   = 0.95
	lowUtilization     = 0.5
	largeMixMargin     = 1.25
	estimatedShareCap  = 0.30
	sparseHistoryLimit = 5
)

// Keys of CompletionRange.Drivers. The PWA and the CLI map each key to text in
// the operator's language.
const (
	driverUtilization  = "utilization"
	driverLargeMix     = "large_prd_mix"
	driverUndecomposed = "undecomposed_backlog"
	driverSparse       = "sparse_history"
)

// progressMethodText describes the assumption behind each completion method.
var progressMethodText = map[string]string{
	methodPRDRate:     "Assumes Automata finish at the rate of the last three active days.",
	methodTaskRate:    "Assumes tasks finish at the average rate over all active days so far.",
	methodTaskRate24h: "Assumes tasks finish at the rate of the last 24 hours of active time.",
	methodWorkerHours: "Assumes each task takes the average worker time of a completed task and workers stay as busy as they have been per active day.",
}

// computeRemaining fills Remaining and Completion. Known work (records that
// exist) and estimated work (rows not filed yet, records with no tasks yet) stay
// separate; Combined exists because the status script prints one total, and it
// is always an estimate.
func computeRemaining(m *ProgressMetrics, in ProgressInput, recs []progressRec) {
	known := RemainingKnown{}
	for _, r := range recs {
		if r.status == PRDCompleted {
			continue
		}
		known.PRDs++
		hasTask := false
		for _, t := range progressTasks(r.prd) {
			hasTask = true
			if t.Status != TaskCompleted {
				known.Tasks++
			}
		}
		if !hasTask {
			known.UndecomposedPRDs++
		}
	}

	est := RemainingEstimated{
		UndecomposedPRDs: known.UndecomposedPRDs,
		PerKind:          map[string]TasksPerPRD{},
	}
	if !m.BacklogAvailable {
		m.NotMeasured = append(m.NotMeasured, notMeasuredBacklog)
	} else {
		est.NewPRDs = newPRDsFromBacklog(in.Backlog)
	}

	est.TasksPerPRD, est.PerKind = measureTasksPerPRD(recs)
	if est.TasksPerPRD.Samples == 0 {
		m.NotMeasured = append(m.NotMeasured, notMeasuredTasksPerPRD)
	} else {
		est.EstimatedTasks = float64(est.NewPRDs+est.UndecomposedPRDs) * est.TasksPerPRD.Mean
	}

	comb := RemainingCombined{
		PRDs:  known.PRDs + est.NewPRDs,
		Tasks: float64(known.Tasks) + est.EstimatedTasks,
		Label: combinedLabel,
	}
	m.Remaining = RemainingWork{Known: known, Estimated: est, Combined: comb}
	m.Completion = completionRange(m, comb)
	m.Inactive.CalendarEstimateDays = copyFloatPtr(m.Completion.CalendarDays)
}

// newPRDsFromBacklog counts the records the open backlog rows will still become.
// An unfiled row becomes one record per phase (at least one). A filed row already
// has records; only phases beyond the linked ones are new, so a row is never
// counted again through a record it already has.
func newPRDsFromBacklog(rows []BacklogRow) int {
	n := 0
	for _, r := range rows {
		if !r.Open {
			continue
		}
		if len(r.PRDIDs) == 0 {
			n += maxInt(r.Phases, 1)
			continue
		}
		n += maxInt(r.Phases-len(r.PRDIDs), 0)
	}
	return n
}

// measureTasksPerPRD measures how many tasks the kept records hold, over those
// with at least one task, overall and per kind. Every kind is always present.
func measureTasksPerPRD(recs []progressRec) (TasksPerPRD, map[string]TasksPerPRD) {
	var all []float64
	byKind := map[string][]float64{PRDKindFeature: nil, PRDKindBug: nil, PRDKindOther: nil}
	for _, r := range recs {
		n := len(progressTasks(r.prd))
		if n == 0 {
			continue
		}
		all = append(all, float64(n))
		k := r.kind
		if _, ok := byKind[k]; !ok {
			k = PRDKindOther
		}
		byKind[k] = append(byKind[k], float64(n))
	}
	per := make(map[string]TasksPerPRD, len(byKind))
	for k, xs := range byKind {
		per[k] = tasksPerPRDStats(xs)
	}
	return tasksPerPRDStats(all), per
}

func tasksPerPRDStats(xs []float64) TasksPerPRD {
	if len(xs) == 0 {
		return TasksPerPRD{}
	}
	s := make([]float64, len(xs))
	copy(s, xs)
	sort.Float64s(s)
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	med, _ := Median(s)
	return TasksPerPRD{
		Mean:    sum / float64(len(s)),
		Median:  med,
		Min:     s[0],
		Max:     s[len(s)-1],
		Samples: len(s),
	}
}

// completionRange turns the combined remaining work and the measured rates into
// days of active time. A method whose divisor is zero is nil.
func completionRange(m *ProgressMetrics, comb RemainingCombined) CompletionRange {
	c := CompletionRange{
		Method:  make(map[string]string, len(progressMethodText)),
		Drivers: []string{},
	}
	for k, v := range progressMethodText {
		c.Method[k] = v
	}

	tp := m.Throughput
	c.ByPRDRateDays = divPtr(float64(comb.PRDs), tp.PRDsPerDay3d)
	c.ByTaskRateDays = divPtr(comb.Tasks, tp.TasksPerDay)
	if tp.TasksLast24h > 0 {
		active24 := ActiveHours(m.GeneratedAt.Add(-progressOneDay), m.GeneratedAt, m.Inactive.Windows) / 24.0
		c.ByTaskRate24hDays = finitePtr(comb.Tasks / float64(tp.TasksLast24h) * active24)
	}
	// Mean hours per completed task is the worked hours over every completed task,
	// as the status script computes it.
	if m.Totals.TasksDone > 0 && m.Totals.WorkHours > 0 && m.Utilization.WorkerHoursPerDay > 0 {
		hoursPerTask := m.Totals.WorkHours / float64(m.Totals.TasksDone)
		c.ByWorkerHoursDays = finitePtr(comb.Tasks * hoursPerTask / m.Utilization.WorkerHoursPerDay)
	}
	if m.ActiveDays > 0 && m.Inactive.ShareOfCalendar < maxCalendarShare && c.ByTaskRateDays != nil {
		c.CalendarDays = finitePtr(*c.ByTaskRateDays / (1.0 - m.Inactive.ShareOfCalendar))
	}

	for _, v := range []*float64{c.ByPRDRateDays, c.ByTaskRateDays, c.ByTaskRate24hDays, c.ByWorkerHoursDays} {
		if v == nil {
			continue
		}
		if c.LowDays == nil || *v < *c.LowDays {
			c.LowDays = copyFloatPtr(v)
		}
		if c.HighDays == nil || *v > *c.HighDays {
			c.HighDays = copyFloatPtr(v)
		}
	}

	est := m.Remaining.Estimated
	if m.Utilization.Ratio < lowUtilization {
		c.Drivers = append(c.Drivers, driverUtilization)
	}
	if est.TasksPerPRD.Mean > est.TasksPerPRD.Median*largeMixMargin {
		c.Drivers = append(c.Drivers, driverLargeMix)
	}
	if comb.Tasks > 0 && est.EstimatedTasks > estimatedShareCap*comb.Tasks {
		c.Drivers = append(c.Drivers, driverUndecomposed)
	}
	if m.Totals.Done < sparseHistoryLimit {
		c.Drivers = append(c.Drivers, driverSparse)
	}
	return c
}

func divPtr(num, den float64) *float64 {
	if den == 0 {
		return nil
	}
	return finitePtr(num / den)
}

// finitePtr returns &v, or nil when v is NaN or infinite, so JSON never holds a
// non-finite number.
func finitePtr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func copyFloatPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
