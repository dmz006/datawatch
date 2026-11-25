// A command schedule created with a session name outlives the session it first
// resolved to; a schedule created by id does not.

package session

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const idlePane = "❯ \n"

// addNamedHolder saves a session called name with its own pane.
func addNamedHolder(t *testing.T, mgr *Manager, f *FakeTmux, id, name string, state State, pane string) *Session {
	t.Helper()
	now := time.Now()
	sess := &Session{
		ID: id, FullID: "testhost-" + id, Hostname: "testhost", Name: name,
		TmuxSession: "cs-" + id, BackendFamily: "claude-code",
		State: state, CreatedAt: now, UpdatedAt: now,
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	f.mu.Lock()
	f.Pane[sess.TmuxSession] = pane
	f.mu.Unlock()
	return sess
}

func newNameSchedEnv(t *testing.T) (*Manager, *FakeTmux, *ScheduleStore) {
	t.Helper()
	mgr := newTestManager(t)
	f := mgr.WithFakeTmux()
	sched := newSchedT(t)
	mgr.SetScheduleStore(sched)
	return mgr, f, sched
}

// typedInto returns the texts typed into one tmux session.
func typedInto(f *FakeTmux, tmuxSession string) []string {
	var out []string
	for _, c := range typedCalls(f) {
		if c.Session == tmuxSession && len(c.Args) > 0 {
			out = append(out, c.Args[0])
		}
	}
	return out
}

func wantState(t *testing.T, sched *ScheduleStore, id, want string) *ScheduledCommand {
	t.Helper()
	got, ok := sched.Get(id)
	if !ok {
		t.Fatalf("schedule %s is gone", id)
	}
	if got.State != want {
		t.Fatalf("schedule %s state = %q, want %q", id, got.State, want)
	}
	return got
}

func addCron(t *testing.T, sched *ScheduleStore, sessionID, name string) *ScheduledCommand {
	t.Helper()
	sc, err := sched.AddFull(AddOptions{
		SessionID: sessionID, SessionName: name, Command: "check the queue",
		CronExpr: "*/5 * * * *", RunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// makeDue moves a schedule's next fire into the past.
func makeDue(t *testing.T, sched *ScheduleStore, id string) {
	t.Helper()
	if err := sched.Update(id, "", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestNameCronSurvivesKilledTargetAndReachesNextHolder(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	old := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateWaitingInput, idlePane)
	sc := addCron(t, sched, old.FullID, "supervisor")

	if err := mgr.EndSession(old.FullID, EndReason{Caller: "test", Reason: "replaced"}); err != nil {
		t.Fatal(err)
	}
	wantState(t, sched, sc.ID, SchedPending)
	makeDue(t, sched, sc.ID)
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, sc.ID, SchedPending)
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed with no live holder: %+v", calls)
	}

	next := addNamedHolder(t, mgr, f, "bb02", "supervisor", StateRunning, idlePane)
	makeDue(t, sched, sc.ID)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, next.TmuxSession); len(got) != 1 || got[0] != "check the queue" {
		t.Fatalf("typed into the new holder = %q", got)
	}
	if got := wantState(t, sched, sc.ID, SchedPending); !got.RunAt.After(time.Now()) {
		t.Fatalf("cron not re-armed: run_at %s", got.RunAt)
	}
}

func TestNameCronSurvivesDeletedTarget(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	old := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
	sc := addCron(t, sched, old.FullID, "supervisor")

	if err := mgr.Delete(old.FullID, false); err != nil {
		t.Fatal(err)
	}
	wantState(t, sched, sc.ID, SchedPending)
	next := addNamedHolder(t, mgr, f, "bb02", "supervisor", StateWaitingInput, idlePane)
	makeDue(t, sched, sc.ID)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, next.TmuxSession); len(got) != 1 {
		t.Fatalf("typed into the new holder = %q", got)
	}
}

func TestNameCronSurvivesTargetThatEndedByItself(t *testing.T) {
	for _, end := range []State{StateComplete, StateFailed, StateKilled} {
		t.Run(string(end), func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			old := addNamedHolder(t, mgr, f, "aa01", "supervisor", end, idlePane)
			sc := addCron(t, sched, old.FullID, "supervisor")
			makeDue(t, sched, sc.ID)
			mgr.processScheduledItems(t.Context())
			wantState(t, sched, sc.ID, SchedPending)
			if calls := typedCalls(f); len(calls) != 0 {
				t.Fatalf("typed into an ended session: %+v", calls)
			}
		})
	}
}

func TestNameOnlyScheduleWithNoSessionStaysPending(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sc, err := sched.AddFull(AddOptions{SessionName: "later", Command: "hello", RunAt: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	mgr.processScheduledItems(t.Context())
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, sc.ID, SchedPending)

	holder := addNamedHolder(t, mgr, f, "bb02", "later", StateRunning, idlePane)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, holder.TmuxSession); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("typed = %q", got)
	}
	wantState(t, sched, sc.ID, SchedDone)
}

func TestNameOnInputScheduleReachesNextHolderWhenItAsks(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	old := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, err := sched.AddFull(AddOptions{SessionID: old.FullID, SessionName: "worker", Command: "next step"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EndSession(old.FullID, EndReason{Caller: "test", Reason: "replaced"}); err != nil {
		t.Fatal(err)
	}
	wantState(t, sched, sc.ID, SchedPending)

	next := addNamedHolder(t, mgr, f, "bb02", "worker", StateRunning, idlePane)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, next.TmuxSession); len(got) != 0 {
		t.Fatalf("typed before the holder asked for input: %q", got)
	}
	mgr.store.Mutate(func() { next.State = StateWaitingInput })
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, next.TmuxSession); len(got) != 1 || got[0] != "next step" {
		t.Fatalf("typed = %q", got)
	}
	wantState(t, sched, sc.ID, SchedDone)
}

// The guard that keeps scheduled input off an approval prompt applies to the
// session a name schedule moves to.
func TestNameScheduleNeverTypesIntoNewHoldersPrompt(t *testing.T) {
	for label, pane := range map[string]string{
		"approval":     approvalPane,
		"folder trust": "Do you trust the files in this folder?\n ❯ 1. Yes, I trust this folder\n   2. No, exit\n Enter to confirm · Esc to cancel\n",
	} {
		t.Run(label, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			timed, _ := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "yes", RunAt: time.Now().Add(-time.Minute)})
			onInput, _ := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "1"})
			addNamedHolder(t, mgr, f, "bb02", "supervisor", StateWaitingInput, pane)
			mgr.processScheduledItems(t.Context())
			if calls := typedCalls(f); len(calls) != 0 {
				t.Fatalf("typed into a prompt: %+v", calls)
			}
			wantState(t, sched, timed.ID, SchedPending)
			wantState(t, sched, onInput.ID, SchedPending)
		})
	}
}

// The target's pane dies before its record says so: the send fails, and the
// schedule must not fail with it.
func TestNameScheduleSurvivesSendIntoDeadPane(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	old := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
	sc := addCron(t, sched, old.FullID, "supervisor")
	makeDue(t, sched, sc.ID)
	f.mu.Lock()
	delete(f.Pane, old.TmuxSession)
	f.FailNextOp = map[string]int{"send-keys": 1}
	f.mu.Unlock()
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, sc.ID, SchedPending)
}

// ── unchanged behaviour ─────────────────────────────────────────────────────

func TestIDScheduleIsStillCancelledByKill(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	old := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
	sc := addCron(t, sched, old.FullID, "")
	if err := mgr.EndSession(old.FullID, EndReason{Caller: "test", Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	wantState(t, sched, sc.ID, SchedCancelled)
}

func TestIDScheduleStillFailsWhenTargetEndedOrGone(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	ended := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateComplete, idlePane)
	addNamedHolder(t, mgr, f, "bb02", "supervisor", StateRunning, idlePane)
	a := addCron(t, sched, ended.FullID, "")
	b := addCron(t, sched, "testhost-zz99", "")
	makeDue(t, sched, a.ID)
	makeDue(t, sched, b.ID)
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, a.ID, SchedFailed)
	wantState(t, sched, b.ID, SchedFailed)
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
}

func TestDeadNameScheduleIsNotRevived(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	failed := addCron(t, sched, "testhost-zz99", "supervisor")
	if err := sched.MarkDone(failed.ID, true); err != nil {
		t.Fatal(err)
	}
	cancelled := addCron(t, sched, "testhost-zz99", "supervisor")
	if err := sched.Cancel(cancelled.ID); err != nil {
		t.Fatal(err)
	}
	addNamedHolder(t, mgr, f, "bb02", "supervisor", StateWaitingInput, idlePane)
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, failed.ID, SchedFailed)
	wantState(t, sched, cancelled.ID, SchedCancelled)
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
}

// ── the wait itself ─────────────────────────────────────────────────────────

func alertSink(mgr *Manager) *[]ScheduleAlert {
	var got []ScheduleAlert
	mgr.SetScheduleAlertHook(func(a ScheduleAlert) { got = append(got, a) })
	return &got
}

func TestNameWaitAlertsOnceAndSkipsMissedCronFires(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := alertSink(mgr)
	cron := addCron(t, sched, "testhost-zz99", "supervisor")
	onInput, _ := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "on input"})
	for i := 0; i < 3; i++ {
		makeDue(t, sched, cron.ID)
		mgr.processScheduledItems(t.Context())
	}
	if len(*alerts) != 2 {
		t.Fatalf("alerts = %d, want one for each of the two schedules", len(*alerts))
	}
	for _, a := range *alerts {
		if !a.NoSession || a.Schedule.SessionName != "supervisor" {
			t.Fatalf("alert = %+v", a)
		}
	}
	got := wantState(t, sched, cron.ID, SchedPending)
	if !got.RunAt.After(time.Now()) || got.RearmState != RearmStateWaitingSession || got.LastFireResult != "skipped" || len(got.FireHistory) != 3 {
		t.Fatalf("held cron = run_at %s state %q last %q history %d", got.RunAt, got.RearmState, got.LastFireResult, len(got.FireHistory))
	}
	if text := got.RearmText(); !strings.Contains(text, "waiting_session (name:supervisor)") {
		t.Fatalf("list line = %q", text)
	}
	if w, _ := sched.Get(onInput.ID); w.RearmState != RearmStateWaitingSession {
		t.Fatalf("on-input rearm state = %q", w.RearmState)
	}

	// A holder appears: nothing stale is typed, the next fire is delivered,
	// and a later loss is a new wait with its own alert.
	holder := addNamedHolder(t, mgr, f, "bb02", "supervisor", StateRunning, idlePane)
	mgr.processScheduledItems(t.Context())
	if got := typedInto(f, holder.TmuxSession); len(got) != 0 {
		t.Fatalf("a skipped fire was typed: %q", got)
	}
	makeDue(t, sched, cron.ID)
	mgr.processScheduledItems(t.Context())
	got = wantState(t, sched, cron.ID, SchedPending)
	if got.RearmState != "" || got.LastError != "" || got.LastFireResult != "" || got.NameWaitAlerted {
		t.Fatalf("wait not ended by delivery: %+v", got)
	}
	if err := mgr.EndSession(holder.FullID, EndReason{Caller: "test", Reason: "replaced"}); err != nil {
		t.Fatal(err)
	}
	makeDue(t, sched, cron.ID)
	mgr.processScheduledItems(t.Context())
	if len(*alerts) != 3 {
		t.Fatalf("alerts after a second loss = %d, want 3", len(*alerts))
	}
}

func TestNameWaitIsKeptAcrossRestartAndTheAlertIsOwedWithoutAHook(t *testing.T) {
	mgr, _, _ := newNameSchedEnv(t)
	path := filepath.Join(t.TempDir(), "schedule.json")
	sched, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(sched)
	sc := addCron(t, sched, "", "supervisor")
	makeDue(t, sched, sc.ID)
	mgr.processScheduledItems(t.Context()) // no alert hook yet
	if got, _ := sched.Get(sc.ID); got.NameWaitAlerted {
		t.Fatal("marked alerted with nobody to alert")
	}
	alerts := alertSink(mgr)
	makeDue(t, sched, sc.ID)
	mgr.processScheduledItems(t.Context())
	reloaded, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(reloaded)
	makeDue(t, reloaded, sc.ID)
	mgr.processScheduledItems(t.Context())
	if len(*alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(*alerts))
	}
}

func TestNameRecurrencePastItsDeadlineEndsWhileWaiting(t *testing.T) {
	mgr, _, sched := newNameSchedEnv(t)
	sc, _ := sched.AddFull(AddOptions{
		SessionName: "supervisor", Command: "x", RunAt: time.Now().Add(-time.Second),
		RecurEvery: 3600, RecurUntil: time.Now().Add(time.Minute),
	})
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, sc.ID, SchedDone)
}

func TestScheduleTargetChoice(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	now := time.Now()
	add := func(id, name string, state State, age time.Duration) *Session {
		s := addNamedHolder(t, mgr, f, id, name, state, idlePane)
		mgr.store.Mutate(func() { s.CreatedAt = now.Add(-age) })
		return s
	}
	add("aa01", "supervisor", StateKilled, 5*time.Hour)
	add("aa02", "supervisor", StateStalled, 4*time.Hour)
	add("aa03", "supervisor", StateRateLimited, 3*time.Hour)
	add("aa04", "other", StateRunning, 9*time.Hour)
	sc, _ := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "x"})
	want := func(sc *ScheduledCommand, id string) {
		t.Helper()
		got, ok := mgr.ScheduleTarget(sc)
		if !ok || got.ID != id {
			t.Fatalf("target = %+v ok=%v, want %s", got, ok, id)
		}
	}
	want(sc, "aa03") // only a rate-limited one is live
	young := add("aa05", "supervisor", StateRunning, time.Hour)
	want(sc, "aa05") // running before rate limited
	add("aa06", "supervisor", StateWaitingInput, 2*time.Hour)
	want(sc, "aa06") // then the oldest

	// The session it first resolved to keeps it while it can take input.
	stored, _ := sched.AddFull(AddOptions{SessionID: young.FullID, SessionName: "supervisor", Command: "x"})
	want(stored, "aa05")
	if got := mgr.WaitingInputSchedules(sched, mustSession(t, mgr, "aa06")); len(got) != 1 || got[0].ID != sc.ID {
		t.Fatalf("on-input for another holder = %+v", got)
	}
	mgr.store.Mutate(func() { young.State = StateStalled })
	want(stored, "aa06")
}

func mustSession(t *testing.T, mgr *Manager, id string) *Session {
	t.Helper()
	s, ok := mgr.GetSession(id)
	if !ok {
		t.Fatalf("no session %s", id)
	}
	return s
}

// A send that fails while the session is still there is not a lost target: it
// fails the schedule as before.
func TestNameScheduleSendErrorOnLiveSessionStillFails(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	holder := addNamedHolder(t, mgr, f, "aa01", "supervisor", StateRunning, idlePane)
	sc := addCron(t, sched, holder.FullID, "supervisor")
	f.mu.Lock()
	f.FailNextOp = map[string]int{"send-keys": 1}
	f.mu.Unlock()
	if sent, err := mgr.FireScheduled(sched, holder.FullID, sc); sent || errors.Is(err, ErrScheduleTargetGone) {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
	wantState(t, sched, sc.ID, SchedFailed)
}

func TestReleaseChainedOnlyAfterItsParentIsFinished(t *testing.T) {
	_, _, sched := newNameSchedEnv(t)
	parent := addCron(t, sched, "", "supervisor")
	child, _ := sched.AddFull(AddOptions{SessionName: "supervisor", Command: "then", RunAfterID: parent.ID})
	if err := sched.ReleaseChained(child.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := sched.Get(child.ID); got.RunAfterID != parent.ID {
		t.Fatal("released while the parent still recurs")
	}
	if err := sched.Cancel(parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := sched.ReleaseChained(child.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := sched.Get(child.ID); got.RunAfterID != "" || got.RunAt.IsZero() {
		t.Fatalf("not released: %+v", got)
	}
}
