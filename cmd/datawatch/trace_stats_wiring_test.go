package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/metrics"
	statspkg "github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/trace"
	dto "github.com/prometheus/client_model/go"
)

// counterValue reads a counter or gauge without the testutil package.
func counterValue(m interface{ Write(*dto.Metric) error }) float64 {
	var out dto.Metric
	if err := m.Write(&out); err != nil {
		return -1
	}
	if out.Counter != nil {
		return out.Counter.GetValue()
	}
	return out.Gauge.GetValue()
}

func TestFillTraceStats(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var s statspkg.SystemStats
	fillTraceStats(&s, trace.StoreStats{Sessions: 2, Events: 9, ErrorEvents: 1,
		Appended: 9, AppendErrors: 3, Pruned: 4, LastEventAt: now.Add(-30 * time.Second)}, now)
	if !s.TraceEnabled || s.TraceSessions != 2 || s.TraceEvents != 9 || s.TraceErrorEvents != 1 ||
		s.TraceAppended != 9 || s.TraceAppendErrors != 3 || s.TracePruned != 4 || s.TraceLastEventAge != 30 {
		t.Fatalf("unexpected stats: %+v", s)
	}
}

func TestUpdateTraceMetricsAddsOnlyDeltas(t *testing.T) {
	traceMetricsLast.mu.Lock()
	traceMetricsLast.appended, traceMetricsLast.appendErrors = 0, 0
	traceMetricsLast.mu.Unlock()
	base := counterValue(metrics.TraceEventsTotal)
	s := statspkg.SystemStats{TraceEnabled: true, TraceEvents: 5, TraceSessions: 1, TraceAppended: 5}
	updateTraceMetrics(s)
	updateTraceMetrics(s) // same snapshot again adds nothing
	s.TraceAppended = 8
	updateTraceMetrics(s)
	if got := counterValue(metrics.TraceEventsTotal) - base; got != 8 {
		t.Fatalf("counter advanced by %v, want 8", got)
	}
	if got := counterValue(metrics.TraceStoredSessions); got != 1 {
		t.Fatalf("sessions gauge = %v", got)
	}
	// Tracing off: gauges and counters stay as they were.
	updateTraceMetrics(statspkg.SystemStats{})
	if got := counterValue(metrics.TraceEventsTotal) - base; got != 8 {
		t.Fatalf("counter moved while tracing off: %v", got)
	}
}
