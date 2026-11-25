// The supervisor wake types only into the designated supervisor session. These
// tests drive the daemon's start-up path (startSupervisorWake) with a fake
// session fleet and a fake clock.

package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// CapturePaneScrollback lets the older wireSessions fake satisfy the wake
// path's session interface: its sessions show a working pane.
func (w *wireSessions) CapturePaneScrollback(string, int) (string, error) {
	return wakeWorkingPane, nil
}

const wakeWorkingPane = "❯ watch the build lanes\n\n● Checking the lanes now.\n\n" +
	"──────────────────────────────\n❯ \n──────────────────────────────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

const wakeFinishedPane = "● DATAWATCH_COMPLETE: built the change and wrote the report\n\n✻ Worked for 4m 2s\n\n" +
	"──────────────────────────────\n❯ \n──────────────────────────────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

// wakeFleet is a fake session manager that honours the tag filter and records
// what was typed into which session.
type wakeFleet struct {
	mu    sync.Mutex
	list  []*session.Session
	panes map[string]string
	typed map[string][]string
}

func (f *wakeFleet) ListSessionsFiltered(flt session.SessionFilter) []*session.Session {
	var out []*session.Session
next:
	for _, s := range f.list {
		for _, want := range flt.Tags {
			found := false
			for _, have := range append(append([]string{}, s.Tags...), s.SystemTags...) {
				found = found || have == want
			}
			if !found {
				continue next
			}
		}
		out = append(out, s)
	}
	return out
}

func (f *wakeFleet) SendInput(id, text, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.typed == nil {
		f.typed = map[string][]string{}
	}
	f.typed[id] = append(f.typed[id], text)
	return nil
}

func (f *wakeFleet) CapturePaneScrollback(id string, _ int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panes[id], nil
}

func (f *wakeFleet) setPane(id, pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes[id] = pane
}

func (f *wakeFleet) lines(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typed[id]...)
}

func (f *wakeFleet) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.typed {
		n += len(l)
	}
	return n
}

// twoRoleSupervisors is the observed fleet: a finished build session launched
// with the supervisor role (listed first, waiting for input forever) and the
// real supervisor.
func twoRoleSupervisors() *wakeFleet {
	role := []string{"role:supervisor"}
	return &wakeFleet{
		list: []*session.Session{
			{ID: "bld1", FullID: "host-bld1", State: session.StateWaitingInput, SystemTags: role, LaunchRole: "supervisor"},
			{ID: "sup1", FullID: "host-sup1", State: session.StateWaitingInput, SystemTags: role, LaunchRole: "supervisor"},
		},
		panes: map[string]string{"host-bld1": wakeFinishedPane, "host-sup1": wakeWorkingPane},
	}
}

type steppedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *steppedClock) AfterFunc(time.Duration, func()) func() bool {
	return func() bool { return true }
}
func (c *steppedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type targetRig struct {
	t     *testing.T
	src   *wireEventSource
	fleet *wakeFleet
	clock *steppedClock
	runs  int
	wait  func()
}

// newTargetRig starts the wake with the given designated session ("" = none),
// no coalesce window and the default 20 minute fallback interval.
func newTargetRig(t *testing.T, fleet *wakeFleet, designated string) *targetRig {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{
		supervisorloop.KeyWakeEnabled: true,
		supervisorloop.KeyWakeKinds: []string{
			autonomouspkg.KindAutomatonStuckRunning, autonomouspkg.KindPriorityStall,
		},
		supervisorloop.KeyWakeCoalesceSeconds: 0,
		"supervisor_loop.wake.session":        designated,
	}
	r := &targetRig{t: t, src: &wireEventSource{}, fleet: fleet, clock: &steppedClock{now: time.Unix(100000, 0)}}
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	ctl := startSupervisorWake(ctx, supervisorWakeDeps{
		Subscriber: r.src,
		Settings:   func() supervisorloop.Settings { return supervisorLoopSettingsFrom(cfg) },
		Run: func(supervisorloop.WakeBatch) {
			mu.Lock()
			r.runs++
			mu.Unlock()
		},
		Sessions: fleet,
		Record:   supervisorloop.NewWakeRecord(supervisorLoopClock{}, 0),
		Clock:    r.clock,
	}, time.Hour)
	r.wait = ctl.Wait
	t.Cleanup(func() { cancel(); ctl.Close() })
	return r
}

// stuck emits a stuck-running event; detail changes the condition fingerprint
// without changing what the typed line says.
func (r *targetRig) stuck(automaton, detail string) {
	r.src.emit(autonomouspkg.LifecycleEvent{Kind: autonomouspkg.KindAutomatonStuckRunning, PRDID: automaton, Details: detail})
	r.wait()
}

func (r *targetRig) stall(automaton, blocker, detail string) {
	r.src.emit(autonomouspkg.LifecycleEvent{Kind: autonomouspkg.KindPriorityStall, PRDID: automaton, NewStatus: blocker, Details: detail})
	r.wait()
}

func TestSupervisorWakeTypesOnlyIntoTheDesignatedSession(t *testing.T) {
	fleet := twoRoleSupervisors()
	r := newTargetRig(t, fleet, "host-sup1")
	r.stuck("a1", "x")
	if got := fleet.lines("host-bld1"); len(got) != 0 {
		t.Errorf("the finished build session was typed into: %q", got)
	}
	if got := fleet.lines("host-sup1"); len(got) != 1 {
		t.Errorf("the designated supervisor got %d lines, want 1", len(got))
	}
}

func TestSupervisorWakeDesignatedByShortID(t *testing.T) {
	fleet := twoRoleSupervisors()
	r := newTargetRig(t, fleet, "sup1")
	r.stuck("a1", "x")
	if len(fleet.lines("host-sup1")) != 1 || fleet.total() != 1 {
		t.Errorf("typed = %v, want one line to host-sup1", fleet.typed)
	}
}

func TestSupervisorWakeNoDesignatedSessionTypesNothing(t *testing.T) {
	for _, designated := range []string{"", "host-gone"} {
		fleet := twoRoleSupervisors()
		fleet.setPane("host-bld1", wakeWorkingPane)
		r := newTargetRig(t, fleet, designated)
		r.stuck("a1", "x")
		if n := fleet.total(); n != 0 {
			t.Errorf("designated %q: %d lines typed, want 0: %v", designated, n, fleet.typed)
		}
		if r.runs != 1 {
			t.Errorf("designated %q: the pass ran %d times, want 1", designated, r.runs)
		}
	}
}

func TestSupervisorWakeSkipsSessionEndingWithCompletionMarker(t *testing.T) {
	fleet := twoRoleSupervisors()
	fleet.setPane("host-sup1", wakeFinishedPane)
	r := newTargetRig(t, fleet, "host-sup1")
	r.stuck("a1", "x")
	if n := fleet.total(); n != 0 {
		t.Fatalf("typed into a session whose output ends with a completion marker: %v", fleet.typed)
	}
	if r.runs != 1 {
		t.Fatalf("the pass ran %d times, want 1", r.runs)
	}
	// New work after the marker makes it a target again.
	fleet.setPane("host-sup1", wakeFinishedPane[:strings.Index(wakeFinishedPane, "──")]+wakeWorkingPane)
	r.stuck("a2", "x")
	if len(fleet.lines("host-sup1")) != 1 {
		t.Fatalf("typed = %v, want one line after new work", fleet.typed)
	}
}

func TestSupervisorWakeDoesNotRetypeAnIdenticalBatch(t *testing.T) {
	fleet := twoRoleSupervisors()
	r := newTargetRig(t, fleet, "host-sup1")
	r.stuck("a1", "waiting 10m")
	r.clock.advance(5 * time.Minute)
	r.stuck("a1", "waiting 15m")
	r.stuck("a2", "waiting 1m")
	r.clock.advance(time.Minute)
	r.stuck("a1", "waiting 16m") // same content as the first line, still inside the interval
	if got := fleet.lines("host-sup1"); len(got) != 2 {
		t.Fatalf("lines = %q, want a1 once and a2 once", got)
	}
	if r.runs != 4 {
		t.Fatalf("the pass ran %d times, want every wake (4)", r.runs)
	}
	r.clock.advance(20 * time.Minute)
	r.stuck("a1", "waiting 36m")
	if got := fleet.lines("host-sup1"); len(got) != 3 {
		t.Fatalf("after the fallback interval: %q, want a third line", got)
	}
}

func TestSupervisorWakePriorityStallListResentOnlyOnChange(t *testing.T) {
	fleet := twoRoleSupervisors()
	r := newTargetRig(t, fleet, "host-sup1")
	r.stall("a1", "held", "waiting 61m")
	r.clock.advance(time.Hour) // far past the fallback interval
	r.stall("a1", "held", "waiting 121m")
	if got := fleet.lines("host-sup1"); len(got) != 1 {
		t.Fatalf("an unchanged stall list was typed again: %q", got)
	}
	// An unchanged stall list is left out of a batch with other news.
	r.stuck("a9", "x")
	r.clock.advance(time.Hour)
	r.stall("a1", "held", "waiting 181m")
	if got := fleet.lines("host-sup1"); len(got) != 2 {
		t.Fatalf("lines = %q, want the stall once and a9 once", got)
	}
	// A changed blocker is a changed list, and the line says which blocker.
	r.stall("a1", "lane_full", "waiting 182m")
	got := fleet.lines("host-sup1")
	if len(got) != 3 || !strings.Contains(got[2], "automaton=a1") || !strings.Contains(got[2], "blocker=lane_full") {
		t.Fatalf("lines = %q, want a third line naming the new blocker", got)
	}
}

// The status view names the same target the wake types into, with the reason.
func TestSupervisorWakeTargetReasons(t *testing.T) {
	fleet := twoRoleSupervisors()
	fleet.list = append(fleet.list,
		&session.Session{ID: "dup1", FullID: "a-dup1", State: session.StateRunning},
		&session.Session{ID: "dup1", FullID: "b-dup1", State: session.StateRunning},
		&session.Session{ID: "mute", FullID: "host-mute", State: session.StateRunning})
	for designated, want := range map[string]string{
		"host-sup1": supervisorloop.WakeTargetOK,
		"host-bld1": supervisorloop.WakeTargetCompleted,
		"dup1":      supervisorloop.WakeTargetAmbiguous,
		"host-mute": supervisorloop.WakeTargetUnreadable,
	} {
		got := supervisorWakeTarget(fleet, designated)
		if got.Session != designated || got.Code != want || got.Reason == "" || (got.Target != "") != (want == supervisorloop.WakeTargetOK) {
			t.Errorf("designated %q: %+v, want code %s", designated, got, want)
		}
	}
}

// The daemon's stall list covers stalls that are not in the batch: a stall that
// dropped out of a batch and came back is not news.
func TestSupervisorWakeStallListComesFromTheDaemon(t *testing.T) {
	m := supervisorloop.NewWakeSendMemory()
	now := time.Unix(1000, 0)
	ev := func(id string) supervisorloop.WakeEvent {
		return supervisorloop.WakeEvent{Kind: autonomouspkg.KindPriorityStall, AutomatonID: id, Fingerprint: "held|{}"}
	}
	both := []string{supervisorloop.WakeStallEntry("a1", "held"), supervisorloop.WakeStallEntry("a2", "held")}
	keep, typed := m.Select("s", []supervisorloop.WakeEvent{ev("a1"), ev("a2")}, both, time.Minute, now)
	if len(keep) != 2 {
		t.Fatalf("first list: %d events", len(keep))
	}
	typed()
	if keep, _ := m.Select("s", []supervisorloop.WakeEvent{ev("a2")}, both, time.Minute, now.Add(time.Hour)); len(keep) != 0 {
		t.Fatalf("unchanged list typed again: %+v", keep)
	}
	// Another session has not been told.
	if keep, _ := m.Select("t", []supervisorloop.WakeEvent{ev("a2")}, both, time.Minute, now.Add(time.Hour)); len(keep) != 1 {
		t.Fatalf("a new target got %d events, want 1", len(keep))
	}
	// A line that was not sent (no commit) is offered again.
	if keep, _ := m.Select("t", []supervisorloop.WakeEvent{ev("a2")}, both, time.Minute, now.Add(time.Hour)); len(keep) != 1 {
		t.Fatalf("an unsent line was remembered as typed")
	}
}
