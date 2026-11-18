package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type fakeWakeClock struct{ now time.Time }

func (c *fakeWakeClock) Now() time.Time { return c.now }
func (c *fakeWakeClock) AfterFunc(time.Duration, func()) func() bool {
	return func() bool { return true }
}

// fakeEventSource is a lifecycle event source whose subscriptions can be
// cancelled and counted.
type fakeEventSource struct {
	mu   sync.Mutex
	next int
	subs map[int]fakeSub
}

type fakeSub struct {
	filter autonomous.LifecycleFilter
	fn     func(autonomous.LifecycleEvent)
}

func (s *fakeEventSource) Subscribe(f autonomous.LifecycleFilter, fn func(autonomous.LifecycleEvent)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs == nil {
		s.subs = map[int]fakeSub{}
	}
	id := s.next
	s.next++
	s.subs[id] = fakeSub{f, fn}
	return func() { s.mu.Lock(); delete(s.subs, id); s.mu.Unlock() }
}

func (s *fakeEventSource) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.subs) }

func (s *fakeEventSource) emit(ev autonomous.LifecycleEvent) {
	s.mu.Lock()
	subs := make([]fakeSub, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		if sub.filter.Matches(ev) {
			sub.fn(ev)
		}
	}
}

type wakeRig struct {
	src     *fakeEventSource
	ctl     *SupervisorWakeController
	record  *supervisorloop.WakeRecord
	runs    atomic.Int32
	sent    atomic.Int32
	wake    supervisorloop.WakeSettings
	wakeMu  sync.Mutex
	session string
}

func newWakeRig(t *testing.T, wake supervisorloop.WakeSettings) *wakeRig {
	t.Helper()
	r := &wakeRig{src: &fakeEventSource{}, wake: wake}
	clock := &fakeWakeClock{now: time.Unix(5000, 0)}
	r.record = supervisorloop.NewWakeRecord(clock, 0)
	r.ctl = NewSupervisorWakeController(SupervisorWakeControllerConfig{
		Subscriber: r.src,
		Settings: func() supervisorloop.Settings {
			r.wakeMu.Lock()
			defer r.wakeMu.Unlock()
			s := supervisorloop.DefaultSettings()
			s.Wake = r.wake
			return s
		},
		Run:       func(supervisorloop.WakeBatch) { r.runs.Add(1) },
		Session:   func() string { return r.session },
		SendInput: func(string, string) error { r.sent.Add(1); return nil },
		Clock:     clock,
		Record:    r.record,
	})
	t.Cleanup(r.ctl.Close)
	return r
}

func (r *wakeRig) setWake(w supervisorloop.WakeSettings) {
	r.wakeMu.Lock()
	r.wake = w
	r.wakeMu.Unlock()
}

func wakeOn(kinds ...string) supervisorloop.WakeSettings {
	w := supervisorloop.DefaultWakeSettings()
	w.Enabled = true
	w.Kinds = kinds
	w.CoalesceSeconds = 0
	return w
}

var wakeEvent = autonomous.LifecycleEvent{ID: "e1", Kind: autonomous.KindAutomatonStuckRunning, PRDID: "p1", At: time.Unix(5000, 0)}

// With the wake off, registering nothing means an event does nothing.
func TestSupervisorWakeOffDoesNothing(t *testing.T) {
	r := newWakeRig(t, supervisorloop.DefaultWakeSettings())
	if r.ctl.Active() || r.src.count() != 0 {
		t.Fatalf("wake is off but %d subscriptions are registered", r.src.count())
	}
	r.src.emit(wakeEvent)
	r.ctl.Wait()
	if r.runs.Load() != 0 || r.record.Status().EventCount != 0 {
		t.Fatalf("an event ran the loop %d times with the wake off", r.runs.Load())
	}
}

// Turning the wake on at runtime registers the set; a subscribed event runs the
// loop once and is recorded; an unsubscribed kind does not; turning it off
// removes the subscriptions again.
func TestSupervisorWakeFollowsLiveSettings(t *testing.T) {
	r := newWakeRig(t, supervisorloop.DefaultWakeSettings())

	r.setWake(wakeOn(autonomous.KindAutomatonStuckRunning))
	if !r.ctl.Reconcile() || r.src.count() != 1 {
		t.Fatalf("wake turned on: active=%v subscriptions=%d, want true/1", r.ctl.Active(), r.src.count())
	}
	r.src.emit(autonomous.LifecycleEvent{ID: "other", Kind: autonomous.KindBurnGuardRungChanged})
	r.ctl.Wait()
	if r.runs.Load() != 0 {
		t.Fatalf("an unsubscribed kind ran the loop")
	}
	r.src.emit(wakeEvent)
	r.ctl.Wait()
	if r.runs.Load() != 1 {
		t.Fatalf("a subscribed event ran the loop %d times, want 1", r.runs.Load())
	}
	if st := r.record.Status(); st.EventCount != 1 {
		t.Fatalf("wake record counts %d events, want 1", st.EventCount)
	}
	if r.sent.Load() != 0 {
		t.Fatalf("typed into a session while none is running")
	}

	// A running supervisor session also gets the summary line.
	r.session = "sup-1"
	r.src.emit(autonomous.LifecycleEvent{ID: "e2", Kind: autonomous.KindAutomatonStuckRunning, PRDID: "p2"})
	r.ctl.Wait()
	if r.runs.Load() != 2 || r.sent.Load() != 1 {
		t.Fatalf("runs=%d sent=%d, want 2 and 1", r.runs.Load(), r.sent.Load())
	}

	// Changing the kinds replaces the set; turning the wake off removes it.
	r.setWake(wakeOn(autonomous.KindBurnGuardRungChanged))
	r.ctl.Reconcile()
	if r.src.count() != 1 {
		t.Fatalf("kinds changed: %d subscriptions, want 1", r.src.count())
	}
	r.setWake(supervisorloop.DefaultWakeSettings())
	if r.ctl.Reconcile() || r.src.count() != 0 {
		t.Fatalf("wake turned off: still %d subscriptions", r.src.count())
	}
	before := r.runs.Load()
	r.src.emit(autonomous.LifecycleEvent{ID: "e3", Kind: autonomous.KindBurnGuardRungChanged})
	r.ctl.Wait()
	if r.runs.Load() != before {
		t.Fatalf("an event ran the loop after the wake was turned off")
	}
}

// The timer picks up a settings change with no explicit Reconcile.
func TestSupervisorWakeTimerPicksUpChange(t *testing.T) {
	r := newWakeRig(t, supervisorloop.DefaultWakeSettings())
	ctx, cancel := contextWithCancel(t)
	defer cancel()
	r.ctl.Start(ctx, 5*time.Millisecond)
	r.setWake(wakeOn(autonomous.KindAutomatonStuckRunning))
	deadline := time.Now().Add(5 * time.Second)
	for !r.ctl.Active() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !r.ctl.Active() {
		t.Fatal("the reconcile timer never registered the wake set")
	}
}

func contextWithCancel(t *testing.T) (ctx context.Context, cancel context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
