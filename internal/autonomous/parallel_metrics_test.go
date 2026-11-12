package autonomous

import (
	"math"
	"testing"
	"time"
)

func metricsTask(base time.Time, startSec, endSec int, readOnly bool) Task {
	s := base.Add(time.Duration(startSec) * time.Second)
	e := base.Add(time.Duration(endSec) * time.Second)
	return Task{StartedAt: &s, CompletedAt: &e, ReadOnly: readOnly}
}

func TestComputeParallelMetricsKnownDurations(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prd := &PRD{Story: []Story{{Tasks: []Task{
		metricsTask(base, 0, 60, true),
		metricsTask(base, 0, 60, true),
		metricsTask(base, 60, 120, false),
		metricsTask(base, 60, 120, false),
	}}}}
	m := ComputeParallelMetrics(prd)
	if m.TotalTasks != 4 || m.ReadOnlyTasks != 2 || m.ConcurrentTasks != 4 {
		t.Fatalf("counts = %+v", m)
	}
	if m.SerialSeconds != 240 || m.WallClockSeconds != 120 {
		t.Fatalf("times = %+v", m)
	}
	if math.Abs(m.Speedup-2) > 1e-9 {
		t.Fatalf("speedup = %v, want 2", m.Speedup)
	}
}

func TestComputeParallelMetricsSerialRunHasSpeedupOne(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prd := &PRD{Story: []Story{{Tasks: []Task{
		metricsTask(base, 0, 30, false),
		metricsTask(base, 30, 90, false),
	}}}}
	m := ComputeParallelMetrics(prd)
	if m.ConcurrentTasks != 0 || math.Abs(m.Speedup-1) > 1e-9 {
		t.Fatalf("metrics = %+v", m)
	}
}

func TestComputeParallelMetricsZeroTasksSafe(t *testing.T) {
	for _, prd := range []*PRD{nil, {}, {Story: []Story{{}}}} {
		if m := ComputeParallelMetrics(prd); m != (ParallelMetrics{}) {
			t.Fatalf("metrics = %+v, want zero", m)
		}
	}
	// Tasks that never ran count toward the totals but not the timing.
	m := ComputeParallelMetrics(&PRD{Story: []Story{{Tasks: []Task{{ReadOnly: true}, {}}}}})
	if m.TotalTasks != 2 || m.ReadOnlyTasks != 1 || m.Speedup != 0 || m.WallClockSeconds != 0 {
		t.Fatalf("metrics = %+v", m)
	}
}

func TestApplyParallelMetricsPersistsOnPRD(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prd := &PRD{Story: []Story{{Tasks: []Task{metricsTask(base, 0, 10, true), metricsTask(base, 0, 10, false)}}}}
	applyParallelMetrics(prd)
	if prd.TotalTasks != 2 || prd.ReadOnlyTasks != 1 || math.Abs(prd.Speedup-2) > 1e-9 {
		t.Fatalf("prd = %+v", prd)
	}
	applyParallelMetrics(nil)
}
