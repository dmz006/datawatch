// One due fire of a command schedule is typed at most once, whichever of the
// daemon's passes sees it.

package session

import (
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// heldTmux stops the first send inside the keystroke call until the test lets
// it go, so a second pass runs while the first is still typing. Later sends go
// straight through.
type heldTmux struct {
	*FakeTmux
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func holdFirstSend(mgr *Manager, f *FakeTmux) *heldTmux {
	h := &heldTmux{FakeTmux: f, entered: make(chan struct{}), release: make(chan struct{})}
	mgr.tmux = h
	return h
}

func (h *heldTmux) hold() {
	if h.first.CompareAndSwap(false, true) {
		close(h.entered)
		<-h.release
	}
}

func (h *heldTmux) SendKeys(session, keys string) error {
	h.hold()
	return h.FakeTmux.SendKeys(session, keys)
}

func (h *heldTmux) SendKeysWithSettle(session, keys string, settle time.Duration) error {
	h.hold()
	return h.FakeTmux.SendKeysWithSettle(session, keys, settle)
}

// whileTyping starts pass, waits until it is inside its first send, runs
// meanwhile, then lets the send finish and waits for pass to return.
func (h *heldTmux) whileTyping(t *testing.T, pass, meanwhile func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pass()
	}()
	select {
	case <-h.entered:
	case <-done:
		t.Fatal("the first pass typed nothing")
	}
	meanwhile()
	close(h.release)
	<-done
}

func TestTwoSchedulePassesTypeOneDueFireOnce(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for label, opts := range map[string]AddOptions{
		"one-time":  {Command: "run it", RunAt: past},
		"cron":      {Command: "run it", RunAt: past, CronExpr: "*/5 * * * *"},
		"interval":  {Command: "run it", RunAt: past, RecurEvery: 3600},
		"by name":   {Command: "run it", RunAt: past, SessionName: "worker"},
		"on input":  {Command: "run it"},
		"on a name": {Command: "run it", SessionName: "worker"},
	} {
		t.Run(label, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateWaitingInput, idlePane)
			if opts.SessionName == "" {
				opts.SessionID = sess.FullID
			}
			if _, err := sched.AddFull(opts); err != nil {
				t.Fatal(err)
			}
			held := holdFirstSend(mgr, f)
			pass := func() { mgr.processScheduledItems(t.Context()) }
			held.whileTyping(t, pass, pass)
			if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"run it"}) {
				t.Fatalf("typed = %q, want it once", got)
			}
		})
	}
}

// Both passes list what is due before either of them types.
func TestStaleDueListDoesNotTypeAFireAgain(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for label, opts := range map[string]AddOptions{
		"one-time": {Command: "run it", RunAt: past},
		"cron":     {Command: "run it", RunAt: past, CronExpr: "*/5 * * * *"},
	} {
		t.Run(label, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
			opts.SessionID = sess.FullID
			if _, err := sched.AddFull(opts); err != nil {
				t.Fatal(err)
			}
			first, second := sched.DuePending(time.Now()), sched.DuePending(time.Now())
			if sent, err := mgr.FireScheduled(sched, sess.FullID, first[0]); !sent || err != nil {
				t.Fatalf("first: sent=%v err=%v", sent, err)
			}
			if sent, _ := mgr.FireScheduled(sched, sess.FullID, second[0]); sent {
				t.Error("the second pass reports it typed the same fire")
			}
			if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"run it"}) {
				t.Fatalf("typed = %q, want it once", got)
			}
		})
	}
}

// A schedule cancelled after a pass listed it is not typed.
func TestScheduleCancelledAfterListingIsNotTyped(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, err := sched.Add(sess.FullID, "run it", time.Now().Add(-time.Minute), "")
	if err != nil {
		t.Fatal(err)
	}
	due := sched.DuePending(time.Now())
	if err := sched.Cancel(sc.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = mgr.FireScheduled(sched, sess.FullID, due[0])
	if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
		t.Fatalf("typed a cancelled schedule: %q", got)
	}
	wantState(t, sched, sc.ID, SchedCancelled)
}

// Two lines typed into one terminal at once can join into a different command,
// so a second schedule waits while one is being typed into the session.
func TestSecondScheduleWaitsWhileOneIsBeingTypedIntoTheSession(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	past := time.Now().Add(-time.Minute)
	for _, cmd := range []string{"first", "second"} {
		if _, err := sched.Add(sess.FullID, cmd, past, ""); err != nil {
			t.Fatal(err)
		}
	}
	held := holdFirstSend(mgr, f)
	pass := func() { mgr.processScheduledItems(t.Context()) }
	held.whileTyping(t, pass, func() {
		pass()
		if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
			t.Errorf("typed over a send in progress: %q", got)
		}
	})
	if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("typed = %q", got)
	}
}

// A daemon that restarts while a command is being typed reads the schedule
// file as it was at that moment. It must not type the command again.
func TestRestartDuringASendDoesNotTypeItAgain(t *testing.T) {
	mgr, f, _ := newNameSchedEnv(t)
	path := filepath.Join(t.TempDir(), "schedule.json")
	sched, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(sched)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, err := sched.Add(sess.FullID, "run it", time.Now().Add(-time.Minute), "")
	if err != nil {
		t.Fatal(err)
	}
	held := holdFirstSend(mgr, f)
	held.whileTyping(t, func() { mgr.processScheduledItems(t.Context()) }, func() {
		reloaded, err := NewScheduleStore(path)
		if err != nil {
			t.Fatal(err)
		}
		due := reloaded.DuePending(time.Now())
		if len(due) == 1 && due[0].ID == sc.ID {
			_, _ = mgr.FireScheduled(reloaded, sess.FullID, due[0])
		}
		if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
			t.Errorf("the reloaded store typed it: %q", got)
		}
	})
}

// A command chained after another is typed once the other is, by whichever
// pass typed that one.
func TestManagerSchedulePassTypesTheChainedCommand(t *testing.T) {
	for label, parentAt := range map[string]time.Time{"timed": time.Now().Add(-time.Minute), "on input": {}} {
		t.Run(label, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateWaitingInput, idlePane)
			parent, err := sched.Add(sess.FullID, "first", parentAt, "")
			if err != nil {
				t.Fatal(err)
			}
			child, err := sched.Add(sess.FullID, "second", time.Time{}, parent.ID)
			if err != nil {
				t.Fatal(err)
			}
			mgr.processScheduledItems(t.Context())
			mgr.processScheduledItems(t.Context())
			if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"first", "second"}) {
				t.Fatalf("typed = %q", got)
			}
			wantState(t, sched, child.ID, SchedDone)
		})
	}
}

// A send that is refused or fails gives the fire back: a guarded or waiting
// schedule is still due and is typed once when it can be.
func TestRefusedScheduleStaysDueAndIsTypedOnce(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateWaitingInput, approvalPane)
	sc, err := sched.Add(sess.FullID, "run it", time.Now().Add(-time.Minute), "")
	if err != nil {
		t.Fatal(err)
	}
	mgr.processScheduledItems(t.Context())
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
		t.Fatalf("typed into a prompt: %q", got)
	}
	wantState(t, sched, sc.ID, SchedPending)

	f.mu.Lock()
	f.Pane[sess.TmuxSession] = idlePane
	f.mu.Unlock()
	mgr.processScheduledItems(t.Context())
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"run it"}) {
		t.Fatalf("typed = %q, want it once", got)
	}
	wantState(t, sched, sc.ID, SchedDone)
}
