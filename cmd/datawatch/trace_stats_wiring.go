package main

import (
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/metrics"
	statspkg "github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/trace"
)

// fillTraceStats copies the trace store's stats onto a SystemStats snapshot.
// now is injected so the last-event age is testable.
func fillTraceStats(s *statspkg.SystemStats, t trace.StoreStats, now time.Time) {
	s.TraceEnabled = true
	s.TraceSessions = t.Sessions
	s.TraceEvents = t.Events
	s.TraceErrorEvents = t.ErrorEvents
	s.TraceAppended = t.Appended
	s.TraceAppendErrors = t.AppendErrors
	s.TracePruned = t.Pruned
	if !t.LastEventAt.IsZero() {
		if age := int64(now.Sub(t.LastEventAt).Seconds()); age > 0 {
			s.TraceLastEventAge = age
		}
	}
}

// traceMetricsLast holds the counter values last pushed to Prometheus, so
// each collection adds only the difference.
var traceMetricsLast struct {
	mu           sync.Mutex
	appended     uint64
	appendErrors uint64
}

// updateTraceMetrics publishes the tracing fields of a stats snapshot.
func updateTraceMetrics(s statspkg.SystemStats) {
	if !s.TraceEnabled {
		return
	}
	metrics.TraceStoredEvents.Set(float64(s.TraceEvents))
	metrics.TraceStoredSessions.Set(float64(s.TraceSessions))
	l := &traceMetricsLast
	l.mu.Lock()
	defer l.mu.Unlock()
	add := func(prev *uint64, now uint64, inc func(float64)) {
		if now > *prev {
			inc(float64(now - *prev))
		}
		*prev = now
	}
	add(&l.appended, s.TraceAppended, metrics.TraceEventsTotal.Add)
	add(&l.appendErrors, s.TraceAppendErrors, metrics.TraceAppendErrorsTotal.Add)
}
