package autonomous

import "time"

// ParallelMetrics is the parallel-execution summary of one Automaton: how
// many tasks it has, how many were read only, how many ran at the same time
// as another task, the wall-clock span of the run, the sum of the task
// durations (the time a serial run would have taken) and the speedup.
type ParallelMetrics struct {
	TotalTasks       int
	ReadOnlyTasks    int
	ConcurrentTasks  int
	WallClockSeconds float64
	SerialSeconds    float64
	Speedup          float64
}

// ComputeParallelMetrics derives the metrics from the tasks of prd. A task
// counts toward the timing figures only when it has both a start and a
// completion time and the completion is not before the start. Wall-clock is
// the span from the earliest start to the latest completion among those
// tasks; speedup is serial time / wall-clock, and 0 when either is zero. A
// nil PRD or one without tasks yields the zero value.
func ComputeParallelMetrics(prd *PRD) ParallelMetrics {
	var out ParallelMetrics
	if prd == nil {
		return out
	}
	type span struct{ start, end time.Time }
	var spans []span
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			out.TotalTasks++
			if t.ReadOnly {
				out.ReadOnlyTasks++
			}
			if t.StartedAt == nil || t.CompletedAt == nil || t.CompletedAt.Before(*t.StartedAt) {
				continue
			}
			spans = append(spans, span{*t.StartedAt, *t.CompletedAt})
		}
	}
	if len(spans) == 0 {
		return out
	}
	first, last := spans[0].start, spans[0].end
	var serial time.Duration
	for _, s := range spans {
		serial += s.end.Sub(s.start)
		if s.start.Before(first) {
			first = s.start
		}
		if s.end.After(last) {
			last = s.end
		}
	}
	for i := range spans {
		for j := range spans {
			if i != j && spans[i].start.Before(spans[j].end) && spans[j].start.Before(spans[i].end) {
				out.ConcurrentTasks++
				break
			}
		}
	}
	wall := last.Sub(first)
	out.WallClockSeconds = wall.Seconds()
	out.SerialSeconds = serial.Seconds()
	if wall > 0 && serial > 0 {
		out.Speedup = serial.Seconds() / wall.Seconds()
	}
	return out
}

// applyParallelMetrics computes the metrics for prd and stores them on the
// PRD record. The caller persists the PRD.
func applyParallelMetrics(prd *PRD) {
	if prd == nil {
		return
	}
	m := ComputeParallelMetrics(prd)
	prd.TotalTasks = m.TotalTasks
	prd.ReadOnlyTasks = m.ReadOnlyTasks
	prd.ConcurrentTasks = m.ConcurrentTasks
	prd.WallClockSeconds = m.WallClockSeconds
	prd.SerialSeconds = m.SerialSeconds
	prd.Speedup = m.Speedup
}
