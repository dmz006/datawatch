package requestwatch

import (
	"errors"
	"testing"
	"time"
)

// fakeClock is an injected clock; the tests never read the wall clock.
type fakeClock struct{ now time.Time }

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *fakeClock) advance(d time.Duration) time.Time {
	c.now = c.now.Add(d)
	return c.now
}

func testParams() Params {
	return Params{NoProgress: 10 * time.Minute, UseGPU: true, GPUBusyPercent: 10, UnreachableCounts: true}
}

func testIdentity() Identity {
	return Identity{SessionID: "s1", AutomatonID: "a1", TaskID: "t1", Role: RoleWorker, Node: "n1", Model: "m1"}
}

func TestTrackerStartsUnknown(t *testing.T) {
	o := NewTracker(testIdentity()).Observation()
	if o.State != StateUnknown {
		t.Fatalf("state = %q, want unknown", o.State)
	}
	if o.SessionID != "s1" || o.AutomatonID != "a1" || o.TaskID != "t1" || o.Role != RoleWorker || o.Node != "n1" || o.Model != "m1" {
		t.Fatalf("identity not carried: %+v", o)
	}
}

func TestTrackerFirstSampleBaseline(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	o, crossed, recovered := tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, testParams())
	if o.State != StateIdle || o.NoProgressSeconds != 0 || o.LastChange != formatTime(c.now) {
		t.Fatalf("baseline = %+v", o)
	}
	if crossed || recovered || o.OverThreshold {
		t.Fatalf("baseline must not cross: %+v", o)
	}
	if o.LastPoll != formatTime(c.now) {
		t.Fatalf("last poll = %q", o.LastPoll)
	}
}

func TestTrackerFirstSampleBaselineGPUBusy(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	o, _, _ := tr.Observe(c.now, Sample{Fingerprint: "a", GPUKnown: true, GPUPercent: 80}, nil, testParams())
	if o.State != StateGenerating {
		t.Fatalf("state = %q, want generating", o.State)
	}
}

func TestTrackerGenerating(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)

	// A different fingerprint is progress.
	now := c.advance(5 * time.Minute)
	o, _, _ := tr.Observe(now, Sample{Fingerprint: "b"}, nil, p)
	if o.State != StateGenerating || o.NoProgressSeconds != 0 || o.LastChange != formatTime(now) {
		t.Fatalf("fingerprint change = %+v", o)
	}

	// A busy GPU with an unchanged fingerprint is progress too.
	now = c.advance(5 * time.Minute)
	o, _, _ = tr.Observe(now, Sample{Fingerprint: "b", GPUKnown: true, GPUPercent: 10}, nil, p)
	if o.State != StateGenerating || o.NoProgressSeconds != 0 {
		t.Fatalf("gpu at the busy percent = %+v", o)
	}
	if o.Summary != "waiting on a model request: generating" {
		t.Fatalf("summary = %q", o.Summary)
	}
}

func TestTrackerGPUSignalOffAndBelow(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)

	// Below the busy percent is not progress.
	now := c.advance(time.Minute)
	o, _, _ := tr.Observe(now, Sample{Fingerprint: "a", GPUKnown: true, GPUPercent: 9.9}, nil, p)
	if o.State != StateIdle || o.NoProgressSeconds != 60 {
		t.Fatalf("gpu below = %+v", o)
	}

	// An unknown GPU reading is not progress even with a high percent field.
	now = c.advance(time.Minute)
	o, _, _ = tr.Observe(now, Sample{Fingerprint: "a", GPUPercent: 99}, nil, p)
	if o.State != StateIdle || o.NoProgressSeconds != 120 {
		t.Fatalf("gpu unknown = %+v", o)
	}

	// With the signal off a busy GPU is ignored.
	off := p
	off.UseGPU = false
	now = c.advance(time.Minute)
	o, _, _ = tr.Observe(now, Sample{Fingerprint: "a", GPUKnown: true, GPUPercent: 100}, nil, off)
	if o.State != StateIdle || o.NoProgressSeconds != 180 {
		t.Fatalf("gpu signal off = %+v", o)
	}

	// With the signal on it is progress.
	now = c.advance(time.Minute)
	o, _, _ = tr.Observe(now, Sample{Fingerprint: "a", GPUKnown: true, GPUPercent: 100}, nil, p)
	if o.State != StateGenerating || o.NoProgressSeconds != 0 {
		t.Fatalf("gpu signal on = %+v", o)
	}
}

func TestTrackerIdle(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	start := c.now
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	for i := 1; i <= 3; i++ {
		now := c.advance(2 * time.Minute)
		o, _, _ := tr.Observe(now, Sample{Fingerprint: "a"}, nil, p)
		if o.State != StateIdle {
			t.Fatalf("state = %q, want idle", o.State)
		}
		if want := int64(i * 120); o.NoProgressSeconds != want {
			t.Fatalf("age = %d, want %d", o.NoProgressSeconds, want)
		}
		if o.LastChange != formatTime(start) {
			t.Fatalf("last change moved: %q", o.LastChange)
		}
	}
}

func TestTrackerUnreachable(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	now := c.advance(3 * time.Minute)
	o, _, _ := tr.Observe(now, Sample{}, errors.New("connection refused"), p)
	if o.State != StateUnreachable || o.Error != "connection refused" {
		t.Fatalf("unreachable = %+v", o)
	}
	if o.NoProgressSeconds != 180 {
		t.Fatalf("age = %d, want 180 (counts)", o.NoProgressSeconds)
	}
	if o.Summary != "waiting on a model request: node unreachable, no progress for 3 minutes" {
		t.Fatalf("summary = %q", o.Summary)
	}

	// A good poll clears the error.
	now = c.advance(time.Minute)
	o, _, _ = tr.Observe(now, Sample{Fingerprint: "a"}, nil, p)
	if o.Error != "" || o.State != StateIdle {
		t.Fatalf("after recovery of the node = %+v", o)
	}
}

func TestTrackerUnreachableFirstSample(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	o, crossed, _ := tr.Observe(c.now, Sample{}, errors.New("down"), testParams())
	if o.State != StateUnreachable || o.NoProgressSeconds != 0 || crossed {
		t.Fatalf("first-sample error = %+v crossed=%v", o, crossed)
	}
}

func TestTrackerUnreachableCountsOnAndOff(t *testing.T) {
	for _, counts := range []bool{true, false} {
		c := newClock()
		tr := NewTracker(testIdentity())
		p := testParams()
		p.UnreachableCounts = counts
		tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)

		var o Observation
		var crossed bool
		for i := 0; i < 3; i++ {
			o, crossed, _ = tr.Observe(c.advance(5*time.Minute), Sample{}, errors.New("down"), p)
		}
		if counts {
			if o.NoProgressSeconds != 900 || !o.OverThreshold {
				t.Fatalf("counts on: %+v", o)
			}
			if !crossed && !o.OverThreshold {
				t.Fatalf("counts on never crossed")
			}
		} else {
			if o.NoProgressSeconds != 0 || o.OverThreshold || crossed {
				t.Fatalf("counts off: %+v crossed=%v", o, crossed)
			}
		}
	}
}

func TestTrackerUnreachableCountsOnCrosses(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	_, crossed, _ := tr.Observe(c.advance(10*time.Minute), Sample{}, errors.New("down"), p)
	if !crossed {
		t.Fatal("counted unreachable time must cross the threshold")
	}
}

func TestTrackerUnreachableCountsOffRestartsClock(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	p.UnreachableCounts = false
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	tr.Observe(c.advance(8*time.Minute), Sample{Fingerprint: "a"}, nil, p)
	// An unreachable poll restarts the clock.
	tr.Observe(c.advance(time.Minute), Sample{}, errors.New("down"), p)
	o, crossed, _ := tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "a"}, nil, p)
	if o.NoProgressSeconds != 60 || crossed {
		t.Fatalf("clock not restarted: %+v crossed=%v", o, crossed)
	}
}

func TestTrackerThreshold(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	idle := Sample{Fingerprint: "a"}
	tr.Observe(c.now, idle, nil, p)

	// Below the threshold.
	o, crossed, _ := tr.Observe(c.advance(10*time.Minute-time.Second), idle, nil, p)
	if o.OverThreshold || crossed {
		t.Fatalf("below threshold = %+v crossed=%v", o, crossed)
	}
	// Exactly at the threshold: crossed fires.
	o, crossed, _ = tr.Observe(c.advance(time.Second), idle, nil, p)
	if !o.OverThreshold || !crossed {
		t.Fatalf("at threshold = %+v crossed=%v", o, crossed)
	}
	// Above the threshold: still over, but Crossed fires only once.
	o, crossed, _ = tr.Observe(c.advance(time.Minute), idle, nil, p)
	if !o.OverThreshold || crossed {
		t.Fatalf("above threshold = %+v crossed=%v", o, crossed)
	}
	o, crossed, _ = tr.Observe(c.advance(time.Minute), idle, nil, p)
	if !o.OverThreshold || crossed {
		t.Fatalf("further above = %+v crossed=%v", o, crossed)
	}
}

func TestTrackerThresholdZeroNeverFires(t *testing.T) {
	for _, nop := range []time.Duration{0, -time.Minute} {
		c := newClock()
		tr := NewTracker(testIdentity())
		p := testParams()
		p.NoProgress = nop
		tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
		o, crossed, _ := tr.Observe(c.advance(24*time.Hour), Sample{Fingerprint: "a"}, nil, p)
		if o.OverThreshold || crossed {
			t.Fatalf("threshold %v fired: %+v", nop, o)
		}
		o, crossed, _ = tr.Observe(c.advance(time.Hour), Sample{}, errors.New("down"), p)
		if o.OverThreshold || crossed {
			t.Fatalf("threshold %v fired while unreachable: %+v", nop, o)
		}
	}
}

func TestTrackerGeneratingNeverOver(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	// Busy GPU for a long stretch: never over the threshold.
	for i := 0; i < 5; i++ {
		o, crossed, _ := tr.Observe(c.advance(20*time.Minute), Sample{Fingerprint: "a", GPUKnown: true, GPUPercent: 90}, nil, p)
		if o.OverThreshold || crossed {
			t.Fatalf("generating over threshold: %+v", o)
		}
	}
}

func TestTrackerRecovered(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	_, crossed, _ := tr.Observe(c.advance(10*time.Minute), Sample{Fingerprint: "a"}, nil, p)
	if !crossed {
		t.Fatal("expected a crossing")
	}
	// Still idle: not recovered.
	_, _, recovered := tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "a"}, nil, p)
	if recovered {
		t.Fatal("recovered without progress")
	}
	// Progress after the crossing: recovered once.
	o, crossed, recovered := tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "b"}, nil, p)
	if !recovered || crossed || o.OverThreshold || o.State != StateGenerating {
		t.Fatalf("recovery = %+v crossed=%v recovered=%v", o, crossed, recovered)
	}
	_, _, recovered = tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "c"}, nil, p)
	if recovered {
		t.Fatal("recovered twice")
	}
	// A new episode crosses again.
	tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "c"}, nil, p)
	_, crossed, _ = tr.Observe(c.advance(10*time.Minute), Sample{Fingerprint: "c"}, nil, p)
	if !crossed {
		t.Fatal("second episode did not cross")
	}
}

func TestTrackerNoRecoveredWithoutCrossing(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	_, _, recovered := tr.Observe(c.advance(time.Minute), Sample{Fingerprint: "b"}, nil, p)
	if recovered {
		t.Fatal("recovered without a crossing")
	}
}

func TestTrackerParamsReadFreshEachCall(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	p := testParams()
	tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, p)
	o, _, _ := tr.Observe(c.advance(5*time.Minute), Sample{Fingerprint: "a"}, nil, p)
	if o.OverThreshold {
		t.Fatal("over at five minutes with a ten minute threshold")
	}
	p.NoProgress = 5 * time.Minute
	o, crossed, _ := tr.Observe(c.advance(time.Second), Sample{Fingerprint: "a"}, nil, p)
	if !o.OverThreshold || !crossed {
		t.Fatalf("lowered threshold did not apply: %+v", o)
	}
}

func TestTrackerObservationJSONOmitsEmptyError(t *testing.T) {
	c := newClock()
	tr := NewTracker(testIdentity())
	o, _, _ := tr.Observe(c.now, Sample{Fingerprint: "a"}, nil, testParams())
	b, err := jsonMarshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "automaton_id", "task_id", "role", "node", "model", "state", "last_change", "no_progress_seconds", "last_poll", "over_threshold", "summary"} {
		if !containsKey(b, key) {
			t.Fatalf("json missing %q: %s", key, b)
		}
	}
	if containsKey(b, "error") {
		t.Fatalf("empty error not omitted: %s", b)
	}
}
