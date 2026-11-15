package plugins

// Regression tests for the model-node events task when its source is
// absent or failing: Run must not panic and must raise no event.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// errModelUseSource is a source whose ModelUse always fails; it counts
// calls so a test can prove the task really passed over it.
type errModelUseSource struct {
	calls atomic.Int64
	err   error
}

func (s *errModelUseSource) ModelUse(context.Context, string) (*ModelUseReport, error) {
	s.calls.Add(1)
	return nil, s.err
}

// nilSafeModelUseSource is a source type whose methods tolerate a nil
// receiver (the shape of the server's default source); a nil pointer of
// this type stored in the Source interface is the typed-nil case.
type nilSafeModelUseSource struct{}

func (s *nilSafeModelUseSource) ModelUse(context.Context, string) (*ModelUseReport, error) {
	if s == nil {
		return nil, errors.New("no source")
	}
	return nil, errors.New("not found")
}

// runModelNodeEvents runs the task until stop is called and returns a
// func that cancels it and fails the test if Run does not return or
// panics.
func runModelNodeEvents(t *testing.T, e *ModelNodeEvents) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		e.Run(ctx)
	}()
	var stopped bool
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case p := <-done:
			if p != nil {
				t.Fatalf("Run panicked: %v", p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	}
	t.Cleanup(stop)
	return stop
}

// TestModelNodeEvents_NilSource_NoPanicNoEvent — a nil Source disables
// the task: Run returns on its own, without panicking, and nothing is
// recorded.
func TestModelNodeEvents_NilSource_NoPanicNoEvent(t *testing.T) {
	sinks := &eventSinks{}
	rec := newTestRecorder(sinks, ServiceEventOptions{})
	var plugged atomic.Int64
	e := &ModelNodeEvents{
		Recorder: rec,
		Source:   nil,
		Plugins: func(context.Context) []string {
			plugged.Add(1)
			return []string{"imap-mcp"}
		},
		Interval: 10 * time.Millisecond,
	}

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		e.Run(context.Background()) // must return without a cancel
	}()
	select {
	case p := <-done:
		if p != nil {
			t.Fatalf("Run panicked with a nil Source: %v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run with a nil Source did not return")
	}
	if n := plugged.Load(); n != 0 {
		t.Errorf("roster resolved %d times with a nil Source, want 0", n)
	}
	if a, u := sinks.countAlerts(), sinks.countAudits(); a != 0 || u != 0 {
		t.Errorf("events raised with a nil Source: %d alerts, %d audits", a, u)
	}
}

// TestModelNodeEvents_TypedNilSource_NoPanicNoEvent — a nil pointer
// stored in the Source interface (a source that tolerates a nil
// receiver) is passed over without a panic or an event.
func TestModelNodeEvents_TypedNilSource_NoPanicNoEvent(t *testing.T) {
	sinks := &eventSinks{}
	rec := newTestRecorder(sinks, ServiceEventOptions{})
	var passes atomic.Int64
	var src *nilSafeModelUseSource // typed nil inside the interface
	e := &ModelNodeEvents{
		Recorder: rec,
		Source:   src,
		Plugins: func(context.Context) []string {
			passes.Add(1)
			return []string{"imap-mcp"}
		},
		Interval: 10 * time.Millisecond,
	}
	stop := runModelNodeEvents(t, e)
	waitFor(t, 5*time.Second, "several passes over the typed-nil source", func() bool { return passes.Load() >= 3 })
	stop()
	if a, u := sinks.countAlerts(), sinks.countAudits(); a != 0 || u != 0 {
		t.Errorf("events raised for a typed-nil source: %d alerts, %d audits", a, u)
	}
}

// TestModelNodeEvents_SourceError_NoPanicNoEvent — a source whose
// ModelUse returns an error is skipped on every pass: Run keeps going,
// does not panic, and raises no event.
func TestModelNodeEvents_SourceError_NoPanicNoEvent(t *testing.T) {
	sinks := &eventSinks{}
	rec := newTestRecorder(sinks, ServiceEventOptions{})
	src := &errModelUseSource{err: errors.New("boom")}
	e := &ModelNodeEvents{
		Recorder: rec,
		Source:   src,
		Plugins:  func(context.Context) []string { return []string{"imap-mcp", "other"} },
		Interval: 10 * time.Millisecond,
	}
	stop := runModelNodeEvents(t, e)
	// Two plugins per pass: 6 calls means at least three passes ran.
	waitFor(t, 5*time.Second, "several passes over the failing source", func() bool { return src.calls.Load() >= 6 })
	stop()
	if a, u := sinks.countAlerts(), sinks.countAudits(); a != 0 || u != 0 {
		t.Errorf("events raised for a failing source: %d alerts, %d audits", a, u)
	}
}
