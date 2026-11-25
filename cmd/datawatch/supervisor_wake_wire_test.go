package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// wireEventSource is a fake lifecycle event source.
type wireEventSource struct {
	mu   sync.Mutex
	next int
	subs map[int]func(autonomouspkg.LifecycleEvent)
	kind map[int]string
}

func (s *wireEventSource) Subscribe(f autonomouspkg.LifecycleFilter, fn func(autonomouspkg.LifecycleEvent)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs == nil {
		s.subs, s.kind = map[int]func(autonomouspkg.LifecycleEvent){}, map[int]string{}
	}
	id := s.next
	s.next++
	s.subs[id] = fn
	if len(f.Kinds) == 1 {
		s.kind[id] = f.Kinds[0]
	}
	return func() { s.mu.Lock(); delete(s.subs, id); s.mu.Unlock() }
}

func (s *wireEventSource) emit(ev autonomouspkg.LifecycleEvent) {
	s.mu.Lock()
	var fns []func(autonomouspkg.LifecycleEvent)
	for id, fn := range s.subs {
		if k := s.kind[id]; k == "" || k == ev.Kind {
			fns = append(fns, fn)
		}
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

type wireSessions struct {
	list []*session.Session
	sent atomic.Int32
	text atomic.Value
}

func (w *wireSessions) ListSessionsFiltered(f session.SessionFilter) []*session.Session {
	return w.list
}
func (w *wireSessions) SendInput(id, text, source string) error {
	w.sent.Add(1)
	w.text.Store(id + "|" + source + "|" + text)
	return nil
}

// The daemon start-up path (startSupervisorWake, the function wireSupervisorLoop
// calls) wakes the supervisor from a subscribed event only while
// supervisor_loop.wake.enabled is on, reading the real config block live.
func TestStartSupervisorWakeFollowsConfigAndRunsLoop(t *testing.T) {
	cfg := config.DefaultConfig()
	src := &wireEventSource{}
	var runs atomic.Int32
	sessions := &wireSessions{list: []*session.Session{{FullID: "host-sup1", State: session.StateRunning}}}
	rec := supervisorloop.NewWakeRecord(supervisorLoopClock{}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctl := startSupervisorWake(ctx, supervisorWakeDeps{
		Subscriber: src,
		Settings:   func() supervisorloop.Settings { return supervisorLoopSettingsFrom(cfg) },
		Run:        func(supervisorloop.WakeBatch) { runs.Add(1) },
		Sessions:   sessions,
		Record:     rec,
		Clock:      supervisorLoopClock{},
	}, 5*time.Millisecond)
	defer ctl.Close()

	ev := autonomouspkg.LifecycleEvent{ID: "x", Kind: autonomouspkg.KindAutomatonStuckRunning, PRDID: "prd-1"}

	// Default config: the wake is off, an event does nothing.
	src.emit(ev)
	ctl.Wait()
	if runs.Load() != 0 || ctl.Active() {
		t.Fatalf("wake is off by default but the loop ran %d times (active=%v)", runs.Load(), ctl.Active())
	}

	// Turn it on the way an operator does: the supervisor_loop config block.
	cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{
		supervisorloop.KeyWakeEnabled:         true,
		supervisorloop.KeyWakeKinds:           []string{autonomouspkg.KindAutomatonStuckRunning},
		supervisorloop.KeyWakeCoalesceSeconds: 0,
		supervisorloop.KeyWakeSession:         "host-sup1",
	}
	deadline := time.Now().Add(5 * time.Second)
	for !ctl.Active() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !ctl.Active() {
		t.Fatal("turning supervisor_loop.wake.enabled on never registered the wake set")
	}
	src.emit(ev)
	ctl.Wait()
	if runs.Load() != 1 {
		t.Fatalf("a subscribed event ran the loop %d times, want 1", runs.Load())
	}
	if rec.Status().EventCount != 1 {
		t.Fatalf("the wake record counts %d event wakes, want 1", rec.Status().EventCount)
	}
	if sessions.sent.Load() != 1 {
		t.Fatalf("the supervisor session got %d lines, want 1", sessions.sent.Load())
	}
	if got, _ := sessions.text.Load().(string); got == "" || got[:len("host-sup1|supervisor-wake|")] != "host-sup1|supervisor-wake|" {
		t.Fatalf("session input = %q", got)
	}

	// Turn it back off: the set is removed and an event does nothing again.
	cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{supervisorloop.KeyWakeEnabled: false}
	for ctl.Active() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	src.emit(ev)
	ctl.Wait()
	if ctl.Active() || runs.Load() != 1 {
		t.Fatalf("wake turned off: active=%v runs=%d, want false/1", ctl.Active(), runs.Load())
	}
}

func TestSupervisorWakeTargetNeedsALiveDesignatedSession(t *testing.T) {
	s := &wireSessions{list: []*session.Session{
		{FullID: "done", State: session.StateComplete},
		{FullID: "live", State: session.StateWaitingInput},
	}}
	if got := supervisorWakeTarget(s, "live"); got.Target != "live" || got.Code != supervisorloop.WakeTargetOK || got.Reason == "" {
		t.Errorf("target = %+v, want live with a reason", got)
	}
	for _, c := range []struct{ designated, code string }{
		{"", supervisorloop.WakeTargetNoneDesignated},
		{"done", supervisorloop.WakeTargetNotLive},
		{"gone", supervisorloop.WakeTargetNotFound},
	} {
		if got := supervisorWakeTarget(s, c.designated); got.Target != "" || got.Code != c.code || got.Reason == "" {
			t.Errorf("designated %q: target = %+v, want none with code %s", c.designated, got, c.code)
		}
	}
	if got := supervisorWakeTarget(&wireSessions{}, "live"); got.Target != "" {
		t.Errorf("no sessions: %+v, want no target", got)
	}
	if got := supervisorWakeTarget(nil, "live"); got.Target != "" {
		t.Errorf("nil sessions: %+v, want no target", got)
	}
}
