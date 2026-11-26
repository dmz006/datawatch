// A schedule that cannot run any more ends and says so: it is never left
// pending for ever, never dropped without an alert, and never typed late.

package session

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type schedAlerts struct {
	mu  sync.Mutex
	all []ScheduleAlert
}

func watchScheduleAlerts(mgr *Manager) *schedAlerts {
	a := &schedAlerts{}
	mgr.SetScheduleAlertHook(func(x ScheduleAlert) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.all = append(a.all, x)
	})
	return a
}

// forID counts the alerts about schedule id.
func (a *schedAlerts) forID(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, x := range a.all {
		if x.Schedule.ID == id {
			n++
		}
	}
	return n
}

// endLongAgo marks sess ended by itself, an hour ago.
func endLongAgo(t *testing.T, mgr *Manager, sess *Session, state State) {
	t.Helper()
	at := time.Now().Add(-time.Hour)
	mgr.store.Mutate(func() {
		sess.State, sess.UpdatedAt = state, at
		sess.EndedAt = &at
	})
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}
}

func TestKillAlertsEachCancelledSchedule(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := watchScheduleAlerts(mgr)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	onInput, _ := sched.Add(sess.FullID, "next prompt", time.Time{}, "")
	timed, _ := sched.Add(sess.FullID, "later", time.Now().Add(time.Hour), "")
	if err := mgr.EndSession(sess.FullID, EndReason{Caller: "test", Reason: "kill"}); err != nil {
		t.Fatal(err)
	}
	for _, sc := range []*ScheduledCommand{onInput, timed} {
		wantState(t, sched, sc.ID, SchedCancelled)
		if n := alerts.forID(sc.ID); n != 1 {
			t.Errorf("schedule %s: %d alerts, want 1", sc.ID, n)
		}
	}
}

func TestSessionThatEndsByItselfSettlesItsSchedules(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := watchScheduleAlerts(mgr)
	done := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	live := addNamedHolder(t, mgr, f, "aa02", "other", StateRunning, idlePane)
	onInput, _ := sched.Add(done.FullID, "next prompt", time.Time{}, "")
	timed, _ := sched.Add(done.FullID, "later", time.Now().Add(time.Hour), "")
	gone, _ := sched.Add("testhost-zz99", "nobody", time.Time{}, "")
	kept, _ := sched.Add(live.FullID, "still wanted", time.Time{}, "")
	endLongAgo(t, mgr, done, StateComplete)

	mgr.processScheduledItems(t.Context())
	for _, sc := range []*ScheduledCommand{onInput, timed, gone} {
		wantState(t, sched, sc.ID, SchedFailed)
		if n := alerts.forID(sc.ID); n != 1 {
			t.Errorf("schedule %s: %d alerts, want 1", sc.ID, n)
		}
	}
	wantState(t, sched, kept.ID, SchedPending)
	mgr.processScheduledItems(t.Context())
	if n := alerts.forID(onInput.ID); n != 1 {
		t.Errorf("a second pass alerted again: %d", n)
	}
}

// A session that has only just ended may be brought back by the reconciler.
func TestAJustEndedSessionKeepsItsSchedulesForNow(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	sc, _ := sched.Add(sess.FullID, "next prompt", time.Time{}, "")
	now := time.Now()
	mgr.store.Mutate(func() { sess.State, sess.UpdatedAt, sess.EndedAt = StateFailed, now, &now })
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, sc.ID, SchedPending)
}

func TestFailedParentFailsItsChain(t *testing.T) {
	for _, how := range []string{"failed", "cancelled", "deleted"} {
		t.Run(how, func(t *testing.T) {
			mgr, f, sched := newNameSchedEnv(t)
			alerts := watchScheduleAlerts(mgr)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
			parent, _ := sched.Add(sess.FullID, "first", time.Now().Add(time.Hour), "")
			child, _ := sched.Add(sess.FullID, "second", time.Time{}, parent.ID)
			grandchild, _ := sched.Add(sess.FullID, "third", time.Time{}, child.ID)
			switch how {
			case "failed":
				_ = sched.MarkDone(parent.ID, true)
			case "cancelled":
				_ = sched.Cancel(parent.ID)
			case "deleted":
				_ = sched.Delete(parent.ID)
			}
			if how != "failed" {
				// Cancelled or deleted: the next timer pass fails the chain.
				mgr.processScheduledItems(t.Context())
			}
			for _, sc := range []*ScheduledCommand{child, grandchild} {
				wantState(t, sched, sc.ID, SchedFailed)
				if n := alerts.forID(sc.ID); n != 1 {
					t.Errorf("%s: %d alerts, want 1", sc.ID, n)
				}
			}
		})
	}
}

// The command before it was typed, but this one never was: it is not typed
// late on datawatch's own say-so.
func TestOrphanedChainIsFailedNotTyped(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := watchScheduleAlerts(mgr)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	parent, _ := sched.Add(sess.FullID, "first", time.Now().Add(-time.Hour), "")
	child, _ := sched.Add(sess.FullID, "second", time.Time{}, parent.ID)
	if err := sched.MarkDone(parent.ID, false); err != nil {
		t.Fatal(err)
	}
	sched.mu.Lock()
	for _, e := range sched.entries {
		if e.ID == parent.ID {
			e.DoneAt = time.Now().Add(-time.Hour)
		}
	}
	sched.mu.Unlock()
	mgr.processScheduledItems(t.Context())
	wantState(t, sched, child.ID, SchedFailed)
	if got := typedInto(f, sess.TmuxSession); len(got) != 0 {
		t.Fatalf("typed late: %q", got)
	}
	if n := alerts.forID(child.ID); n != 1 {
		t.Errorf("%d alerts, want 1", n)
	}
}

func TestOneSendErrorKeepsARecurringSchedule(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	alerts := watchScheduleAlerts(mgr)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	cron, err := sched.AddFull(AddOptions{SessionID: sess.FullID, Command: "tick", CronExpr: "*/5 * * * *", RunAt: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	every, _ := sched.AddFull(AddOptions{SessionID: sess.FullID, Command: "tock", RecurEvery: 60, RunAt: time.Now().Add(-time.Minute)})
	once, _ := sched.Add(sess.FullID, "once", time.Now().Add(-time.Minute), "")
	for _, sc := range []*ScheduledCommand{cron, every, once} {
		f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1}
		_, _ = mgr.FireScheduled(sched, sess.FullID, sc)
	}
	for _, sc := range []*ScheduledCommand{cron, every} {
		got := wantState(t, sched, sc.ID, SchedPending)
		if !got.RunAt.After(time.Now()) || got.LastError == "" || got.ConsecutiveFailures != 1 {
			t.Errorf("not re-armed with its failure recorded: %+v", got)
		}
	}
	wantState(t, sched, once.ID, SchedFailed)
	for _, sc := range []*ScheduledCommand{cron, every, once} {
		if n := alerts.forID(sc.ID); n != 1 {
			t.Errorf("%s: %d alerts, want 1", sc.ID, n)
		}
	}
	// The next fire that works clears the count.
	sched.mu.Lock()
	for _, e := range sched.entries {
		if e.ID == every.ID {
			e.RunAt = time.Now().Add(-time.Second)
		}
	}
	sched.mu.Unlock()
	if sent, err := mgr.FireScheduled(sched, sess.FullID, every); !sent || err != nil {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
	if got := wantState(t, sched, every.ID, SchedPending); got.ConsecutiveFailures != 0 || got.LastError != "" {
		t.Errorf("failure not cleared: %+v", got)
	}
}

func TestDaemonRefusesAScheduleThatCanNeverRun(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	mgr.EnableScheduleTargetCheck()
	live := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	ended := addNamedHolder(t, mgr, f, "aa02", "old", StateKilled, idlePane)
	for _, opts := range []AddOptions{
		{SessionID: "testhost-zz99", Command: "x"},
		{SessionID: ended.FullID, Command: "x"},
		{SessionID: live.FullID, Command: "x", RunAfterID: "no-such-schedule"},
	} {
		if sc, err := sched.AddFull(opts); err == nil {
			t.Errorf("accepted %+v as %s", opts, sc.ID)
		}
	}
	if _, err := sched.Add(ended.FullID, "x", time.Time{}, ""); err == nil {
		t.Error("Add accepted an ended session")
	}
	parent, err := sched.AddFull(AddOptions{SessionID: live.ID, Command: "by short id"})
	if err != nil {
		t.Fatalf("a live session by short id: %v", err)
	}
	for _, opts := range []AddOptions{
		{SessionID: live.FullID, Command: "then", RunAfterID: parent.ID},
		{SessionName: "not-started-yet", Command: "by name"},
	} {
		if _, err := sched.AddFull(opts); err != nil {
			t.Errorf("refused %+v: %v", opts, err)
		}
	}
}

// ── rate-limit resume ───────────────────────────────────────────────────────

func rateLimitedHolder(t *testing.T, mgr *Manager, f *FakeTmux) *Session {
	t.Helper()
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRateLimited, idlePane)
	f.mu.Lock()
	f.sessions[sess.TmuxSession] = true
	f.mu.Unlock()
	return sess
}

func pendingResumes(sched *ScheduleStore) int {
	n := 0
	for _, sc := range sched.List(SchedPending) {
		if strings.HasPrefix(sc.Command, "The rate limit has reset.") {
			n++
		}
	}
	return n
}

// The daemon's boot order: sessions are resumed before the schedule store is
// opened; the schedule timer starts after it.
func TestBootMakesTheResumeWhateverElseIsScheduled(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess := rateLimitedHolder(t, mgr, f)
	sched := newSchedT(t)
	if _, err := sched.Add(sess.FullID, "an unrelated reminder", time.Now().Add(2*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	mgr.ResumeMonitors(t.Context())
	mgr.SetScheduleStore(sched)
	mgr.StartScheduleTimer(t.Context())
	if n := pendingResumes(sched); n != 1 {
		t.Fatalf("pending resumes = %d, want 1", n)
	}
	mgr.StartScheduleTimer(t.Context())
	if n := pendingResumes(sched); n != 1 {
		t.Fatalf("a second start doubled it: %d", n)
	}
}

// One stored before schedules had a kind counts as the resume it is.
func TestBootDoesNotDoubleAnOlderResume(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	sess := rateLimitedHolder(t, mgr, f)
	if _, err := sched.Add(sess.FullID, "The rate limit has reset. Please read PAUSED.md in your working directory for context on what was in progress, then continue the task.", time.Now().Add(time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	mgr.StartScheduleTimer(t.Context())
	if n := pendingResumes(sched); n != 1 {
		t.Fatalf("pending resumes = %d, want 1", n)
	}
}

// A resume the daemon was typing when it stopped may have arrived.
func TestBootDoesNotRetypeAResumeCutOffByARestart(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess := rateLimitedHolder(t, mgr, f)
	sched := newSchedT(t)
	mgr.SetScheduleStore(sched)
	mgr.StartScheduleTimer(t.Context())
	sched.mu.Lock()
	at := time.Now()
	for _, e := range sched.entries {
		if e.SessionID == sess.FullID {
			e.FiringSince = &at
		}
	}
	_ = sched.save()
	sched.mu.Unlock()
	mgr.StartScheduleTimer(t.Context())
	if n := pendingResumes(sched); n != 0 {
		t.Fatalf("a cut-off resume was made again: %d pending", n)
	}
}

// ── outside changes to the file ─────────────────────────────────────────────

func TestStoreAdoptsOutsideAdditionsWithoutRevivingRemovedOnes(t *testing.T) {
	daemon := newSchedT(t)
	removed, _ := daemon.Add("testhost-aa01", "removed later", time.Now().Add(time.Hour), "")
	outside, err := NewScheduleStore(daemon.path) // holds "removed"
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.Delete(removed.ID); err != nil {
		t.Fatal(err)
	}
	added, err := outside.Add("testhost-aa01", "added outside", time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if n := daemon.AdoptExternal(); n != 1 {
		t.Fatalf("adopted %d, want 1", n)
	}
	if _, ok := daemon.Get(added.ID); !ok {
		t.Fatal("the outside schedule is not loaded")
	}
	if _, ok := daemon.Get(removed.ID); ok {
		t.Fatal("a removed schedule came back")
	}
}

// A save by the daemon must not overwrite what was added outside it.
func TestSaveKeepsAnOutsideAddition(t *testing.T) {
	daemon := newSchedT(t)
	mine, _ := daemon.Add("testhost-aa01", "mine", time.Now().Add(time.Hour), "")
	outside, err := NewScheduleStore(daemon.path)
	if err != nil {
		t.Fatal(err)
	}
	added, err := outside.Add("testhost-aa01", "added outside", time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.Cancel(mine.ID); err != nil {
		t.Fatal(err)
	}
	again, err := NewScheduleStore(daemon.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get(added.ID); !ok {
		t.Fatal("the daemon's save dropped the outside schedule")
	}
}

func TestScheduleTimerHoldsTheDaemonLock(t *testing.T) {
	mgr, _, sched := newNameSchedEnv(t)
	dir := sched.path[:strings.LastIndex(sched.path, string(os.PathSeparator))]
	mgr.StartScheduleTimer(t.Context())
	release, ok, err := TryDaemonLock(dir)
	if err != nil {
		t.Skipf("no file locks here: %v", err)
	}
	if ok {
		release()
		t.Fatal("the lock was free while the schedule timer runs")
	}
}

func TestResumeIsMovedByANewLimitAndNotRemadeOnceCancelled(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	sess := rateLimitedHolder(t, mgr, f)
	sched := newSchedT(t)
	mgr.SetScheduleStore(sched)
	mgr.StartScheduleTimer(t.Context())
	later := time.Now().Add(3 * time.Hour)
	if added, err := sched.ensureRateLimitResume(sess.FullID, later, sess.UpdatedAt, true); added || err != nil {
		t.Fatalf("a second limit added a second resume: added=%v err=%v", added, err)
	}
	pending := sched.List(SchedPending)
	if len(pending) != 1 || !pending[0].RunAt.Equal(later) || pending[0].Kind != SchedKindRateLimitResume {
		t.Fatalf("pending = %+v", pending)
	}
	if err := sched.Cancel(pending[0].ID); err != nil {
		t.Fatal(err)
	}
	mgr.StartScheduleTimer(t.Context())
	if n := pendingResumes(sched); n != 0 {
		t.Fatalf("a cancelled resume came back: %d", n)
	}
}
