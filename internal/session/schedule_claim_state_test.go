// What the claim on a schedule fire leaves behind: after each outcome, after a
// restart, and when it cannot be saved.

package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func wantUnclaimed(t *testing.T, sched *ScheduleStore, id string) {
	t.Helper()
	if sc, ok := sched.Get(id); !ok || sc.FiringSince != nil {
		t.Fatalf("schedule %s still claimed (or gone): %+v", id, sc)
	}
	if len(sched.typing) != 0 {
		t.Fatalf("sessions still held: %v", sched.typing)
	}
}

func TestEveryOutcomeGivesTheClaimBack(t *testing.T) {
	cases := map[string]struct {
		pane, state, want string
		byName, failSend  bool
		filter            string
	}{
		"typed":               {pane: idlePane, want: SchedDone},
		"held by a prompt":    {pane: approvalPane, want: SchedPending},
		"send failed":         {pane: idlePane, failSend: true, want: SchedFailed},
		"name target ended":   {pane: idlePane, byName: true, state: string(StateKilled), want: SchedPending},
		"filter may not type": {pane: idlePane, filter: "gone", want: SchedCancelled},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			state := StateRunning
			if c.state != "" {
				state = State(c.state)
			}
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", state, c.pane)
			opts := AddOptions{SessionID: sess.FullID, Command: "run it", RunAt: time.Now(), FilterID: c.filter}
			if c.byName {
				opts.SessionName = "worker"
			}
			sc, err := sched.AddFull(opts)
			if err != nil {
				t.Fatal(err)
			}
			if c.failSend {
				f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1}
			}
			_, _ = mgr.FireScheduled(sched, sess.FullID, sc)
			wantState(t, sched, sc.ID, c.want)
			wantUnclaimed(t, sched, sc.ID)
		})
	}
}

// The claim is in the file before any key is sent.
func TestClaimIsSavedBeforeTypingAndClearedAfter(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, err := sched.Add(sess.FullID, "run it", time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	onDisk := func() *ScheduledCommand {
		t.Helper()
		again, err := NewScheduleStore(sched.path)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := again.Get(sc.ID)
		if !ok {
			t.Fatal("schedule not in the file")
		}
		return got
	}
	held := holdFirstSend(mgr, f)
	held.whileTyping(t, func() { mgr.processScheduledItems(t.Context()) }, func() {
		if got := onDisk(); got.FiringSince == nil || got.State != SchedPending {
			t.Errorf("in the file while typing: %+v", got)
		}
	})
	if got := onDisk(); got.FiringSince != nil || got.State != SchedDone {
		t.Fatalf("in the file after typing: %+v", got)
	}
}

func TestFireWhoseClaimCannotBeSavedIsNotTyped(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, err := sched.Add(sess.FullID, "run it", time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	good := sched.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sched.path = filepath.Join(blocker, "schedule.json")
	if sent, err := mgr.FireScheduled(sched, sess.FullID, sc); sent || !errors.Is(err, ErrFireDeferred) {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
	if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
		t.Fatalf("typed without a saved claim: %q", got)
	}
	sched.path = good
	wantState(t, sched, sc.ID, SchedPending)
	wantUnclaimed(t, sched, sc.ID)
	if sent, err := mgr.FireScheduled(sched, sess.FullID, sc); !sent || err != nil {
		t.Fatalf("once the file can be saved: sent=%v err=%v", sent, err)
	}
}

func TestStartSettlesTheFiresARestartCutOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	before, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	add := func(opts AddOptions) *ScheduledCommand {
		t.Helper()
		opts.SessionID, opts.Command, opts.RunAt = "testhost-aa01", "run it", past
		sc, err := before.AddFull(opts)
		if err != nil {
			t.Fatal(err)
		}
		return sc
	}
	oneTime := add(AddOptions{})
	cron := add(AddOptions{CronExpr: "*/5 * * * *"})
	lastOne := add(AddOptions{RecurEvery: 3600, RecurUntil: time.Now().Add(time.Minute)})
	cancelled := add(AddOptions{})
	waiting := add(AddOptions{})
	for i, sc := range []*ScheduledCommand{oneTime, cron, lastOne, cancelled} {
		// One session each: claims on one session take turns.
		if _, _, err := before.claimFire(sc.ID, string(rune('a'+i)), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := before.Cancel(cancelled.ID); err != nil {
		t.Fatal(err)
	}

	mgr, f, _ := newNameSchedEnv(t)
	sched, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(sched)
	var alerted []string
	mgr.SetScheduleAlertHook(func(a ScheduleAlert) {
		if a.Interrupted {
			alerted = append(alerted, a.Schedule.ID)
		}
	})
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr.StartScheduleTimer(ctx)

	if want := []string{oneTime.ID, cron.ID, lastOne.ID}; !slices.Equal(alerted, want) {
		t.Fatalf("alerted %v, want %v", alerted, want)
	}
	got := wantState(t, sched, oneTime.ID, SchedFailed)
	if !strings.Contains(got.RearmText(), "may or may not have arrived") {
		t.Errorf("one-time list text: %s", got.RearmText())
	}
	if got := wantState(t, sched, cron.ID, SchedPending); !got.RunAt.After(time.Now()) || got.LastFireResult != "skipped" {
		t.Fatalf("cron after restart: run_at %s result %q", got.RunAt, got.LastFireResult)
	}
	wantState(t, sched, lastOne.ID, SchedDone)
	wantState(t, sched, cancelled.ID, SchedCancelled)
	for _, sc := range []*ScheduledCommand{oneTime, cron, lastOne, cancelled} {
		wantUnclaimed(t, sched, sc.ID)
	}

	// Only the fire that was never claimed is typed.
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, sess.TmuxSession); len(got) != 1 {
		t.Fatalf("typed = %q, want only the unclaimed fire", got)
	}
	wantState(t, sched, waiting.ID, SchedDone)

	// The cron's next fire is delivered and clears the note.
	makeDue(t, sched, cron.ID)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, sess.TmuxSession); len(got) != 2 {
		t.Fatalf("typed = %q, want the next cron fire too", got)
	}
	if got := wantState(t, sched, cron.ID, SchedPending); got.LastError != "" || got.LastFireResult != "" {
		t.Fatalf("note left after a delivery: %q %q", got.LastFireResult, got.LastError)
	}
}

// A chained command that finds another schedule being typed into the session
// has no pass of its own, so it becomes due and is typed by a later one.
func TestChainedCommandWaitsForABusySession(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	parent, _ := sched.Add(sess.FullID, "parent", time.Now(), "")
	child, _ := sched.Add("", "child", time.Time{}, parent.ID)
	if err := sched.MarkDone(parent.ID, false); err != nil {
		t.Fatal(err)
	}
	other, _ := sched.Add(sess.FullID, "other", time.Now(), "")
	held := holdFirstSend(mgr, f)
	held.whileTyping(t, func() { _, _ = mgr.FireScheduled(sched, sess.FullID, other) }, func() {
		mgr.FireChained(sched, sess.FullID, parent.ID)
		wantState(t, sched, child.ID, SchedPending)
	})
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"other", "child"}) {
		t.Fatalf("typed = %q", got)
	}
	wantState(t, sched, child.ID, SchedDone)
}

// Two passes reaching the same chain: the one that comes second leaves the
// command to the first, and does not mark it failed.
func TestChainedCommandTakenByAnotherPassIsLeftToIt(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	parent, _ := sched.Add(sess.FullID, "parent", time.Now(), "")
	child, _ := sched.Add(sess.FullID, "child", time.Time{}, parent.ID)
	if err := sched.MarkDone(parent.ID, false); err != nil {
		t.Fatal(err)
	}
	held := holdFirstSend(mgr, f)
	chain := func() { mgr.FireChained(sched, sess.FullID, parent.ID) }
	held.whileTyping(t, chain, func() {
		chain()
		wantState(t, sched, child.ID, SchedPending)
	})
	if got := typedInto(f, sess.TmuxSession); !slices.Equal(got, []string{"child"}) {
		t.Fatalf("typed = %q", got)
	}
	wantState(t, sched, child.ID, SchedDone)
}

// A pass that finds no holder for a name must not skip the fire another pass is
// typing.
func TestHoldForNameLeavesAClaimedFireAlone(t *testing.T) {
	sched := newSchedT(t)
	due := time.Now().Add(-time.Minute)
	sc, _ := sched.AddFull(AddOptions{SessionName: "worker", Command: "x", CronExpr: "*/5 * * * *", RunAt: due})
	if _, release, err := sched.claimFire(sc.ID, "testhost-aa01", time.Now()); err != nil {
		t.Fatal(err)
	} else {
		defer release()
	}
	if _, alert, err := sched.HoldForName(sc.ID, time.Now(), true); alert || err != nil {
		t.Fatalf("alert=%v err=%v", alert, err)
	}
	if got := wantState(t, sched, sc.ID, SchedPending); !got.RunAt.Equal(due) || got.RearmState != "" {
		t.Fatalf("held a claimed fire: run_at %s state %q", got.RunAt, got.RearmState)
	}
}
