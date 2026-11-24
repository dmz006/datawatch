package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

var rearmNow = time.Date(2026, 11, 23, 10, 0, 30, 0, time.UTC)

func rearmPolicy(mode string, retries int) RearmPolicy {
	return RearmPolicy{Mode: mode, MaxRetries: retries, Backoff: 30 * time.Second, BackoffMax: 4 * time.Minute}
}

func cronSched(t *testing.T) (*ScheduleStore, *ScheduledCommand) {
	return newSpawnSched(t, AddSpawnOptions{Task: "x", ScheduleName: "detector", CronExpr: "0 * * * *", Subprocess: true, OneShot: true})
}

func TestIsStartWait_Classification(t *testing.T) {
	wait := []error{
		fmt.Errorf("%w (20) reached", ErrMaxSessions),
		ErrGlobalCooldown, ErrDaemonDraining, capacity.ErrWaitTimeout,
		fmt.Errorf("capacity: %w: queue full", capacity.ErrWaitTimeout),
		errors.New("max sessions (20) reached"), // the same text arriving from a peer
	}
	for _, e := range wait {
		if !IsStartWait(e) {
			t.Errorf("%v must be a wait, not a failure", e)
		}
	}
	for _, e := range []error{nil, errors.New("tmux exploded"), errors.New("project dir refused")} {
		if IsStartWait(e) {
			t.Errorf("%v must not be a wait", e)
		}
	}
}

// A wait for capacity never marks a schedule failed, whatever the policy and
// however many times it repeats, and it never spends the retry budget.
func TestRecordStartFailure_WaitNeverFailsRecurring(t *testing.T) {
	for _, mode := range []string{"", RearmNextFire, RearmBackoff, RearmStayFailed} {
		store, sc := cronSched(t)
		var out StartFailure
		for i := 0; i < 60; i++ {
			var err error
			out, err = store.RecordStartFailure(sc.ID, fmt.Errorf("%w (20) reached", ErrMaxSessions), rearmNow, rearmPolicy(mode, 1))
			if err != nil {
				t.Fatal(err)
			}
		}
		got, _ := store.Get(sc.ID)
		if got.State != SchedPending || out.GaveUp || !out.Wait || !out.Rearmed {
			t.Fatalf("mode %q: a full session cap must leave the schedule pending: state=%s out=%+v", mode, got.State, out)
		}
		if got.ConsecutiveFailures != 0 || got.ConsecutiveWaits != 60 || got.RearmState != RearmStateWaiting {
			t.Fatalf("mode %q: waits must not count as failures: %+v", mode, got)
		}
		if got.LastFireResult != "deferred" || got.LastError == "" {
			t.Fatalf("mode %q: last result must say deferred with the reason: %+v", mode, got)
		}
	}
}

func TestRecordStartFailure_WaitBacksOffAndNeverPastNextFire(t *testing.T) {
	store, sc := cronSched(t)
	p := rearmPolicy(RearmNextFire, 0)
	var delays []time.Duration
	for i := 0; i < 7; i++ {
		out, err := store.RecordStartFailure(sc.ID, ErrGlobalCooldown, rearmNow, p)
		if err != nil {
			t.Fatal(err)
		}
		delays = append(delays, out.NextAt.Sub(rearmNow))
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 4 * time.Minute, 4 * time.Minute, 4 * time.Minute}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("backoff step %d: want %v got %v (all %v)", i, want[i], delays[i], delays)
		}
	}
	// Hourly cron at 10:00:30: next natural fire is 11:00, so a wait is never
	// pushed beyond it; with a long backoff the natural fire wins.
	long := RearmPolicy{Mode: RearmBackoff, Backoff: 2 * time.Hour, BackoffMax: 3 * time.Hour}
	out, _ := store.RecordStartFailure(sc.ID, ErrGlobalCooldown, rearmNow, long)
	if !out.NextAt.Equal(time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("a wait must not push a recurring schedule past its next fire, got %v", out.NextAt)
	}
}

func TestRecordStartFailure_WaitKeepsOneTimeSchedulePending(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
	out, err := store.RecordStartFailure(sc.ID, fmt.Errorf("%w (20) reached", ErrMaxSessions), rearmNow, DefaultRearmPolicy())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(sc.ID)
	if got.State != SchedPending || !out.Rearmed || !got.RunAt.Equal(rearmNow.Add(30*time.Second)) {
		t.Fatalf("a one-time spawn refused for capacity must wait and retry: %+v out=%+v", got, out)
	}
}

func TestRecordStartFailure_BackoffGivesUpAfterMaxRetries(t *testing.T) {
	store, sc := cronSched(t)
	p := rearmPolicy(RearmBackoff, 3)
	boom := errors.New("tmux exploded")
	for i := 1; i <= 3; i++ {
		out, err := store.RecordStartFailure(sc.ID, boom, rearmNow, p)
		if err != nil || out.GaveUp || !out.Rearmed || out.Consecutive != i {
			t.Fatalf("retry %d: out=%+v err=%v", i, out, err)
		}
		if want := rearmNow.Add(30 * time.Second << (i - 1)); !out.NextAt.Equal(want) {
			t.Fatalf("retry %d: want next %v got %v", i, want, out.NextAt)
		}
	}
	got, _ := store.Get(sc.ID)
	if got.State != SchedPending || got.RearmState != RearmStateRetrying || got.LastError != "tmux exploded" {
		t.Fatalf("while retrying the schedule stays pending: %+v", got)
	}
	out, _ := store.RecordStartFailure(sc.ID, boom, rearmNow, p)
	got, _ = store.Get(sc.ID)
	if !out.GaveUp || out.Rearmed || got.State != SchedFailed || got.RearmState != RearmStateGaveUp {
		t.Fatalf("after max_retries the policy must give up and stay failed: %+v out=%+v", got, out)
	}
}

func TestRecordStartFailure_StayFailedMarksFailedAtOnce(t *testing.T) {
	store, sc := cronSched(t)
	out, _ := store.RecordStartFailure(sc.ID, errors.New("boom"), rearmNow, rearmPolicy(RearmStayFailed, 0))
	got, _ := store.Get(sc.ID)
	if !out.GaveUp || got.State != SchedFailed || got.RearmState != RearmStateGaveUp {
		t.Fatalf("stay_failed: %+v out=%+v", got, out)
	}
}

func TestRecordStartFailure_NextFireDefaultNeverGivesUpOnRecurring(t *testing.T) {
	store, sc := cronSched(t)
	for i := 0; i < 50; i++ {
		out, _ := store.RecordStartFailure(sc.ID, errors.New("boom"), rearmNow, DefaultRearmPolicy())
		if out.GaveUp || !out.Rearmed {
			t.Fatalf("default policy keeps re-arming a recurring schedule: %+v", out)
		}
	}
	got, _ := store.Get(sc.ID)
	if !got.RunAt.Equal(time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("default policy re-arms at the next scheduled time, got %v", got.RunAt)
	}
}

func TestRecordFire_SuccessClearsRearmState(t *testing.T) {
	store, sc := cronSched(t)
	_, _ = store.RecordStartFailure(sc.ID, ErrGlobalCooldown, rearmNow, DefaultRearmPolicy())
	_, _ = store.RecordStartFailure(sc.ID, errors.New("boom"), rearmNow, rearmPolicy(RearmBackoff, 5))
	if err := store.RecordFire(sc.ID, "testhost-s9", "spawned"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(sc.ID)
	if got.RearmState != "" || got.ConsecutiveWaits != 0 || got.ConsecutiveFailures != 0 || got.LastError != "" {
		t.Fatalf("a successful start clears the re-arm state: %+v", got)
	}
}

func TestScheduleStore_RearmStateSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/schedule.json"
	store, _ := NewScheduleStore(path)
	sc, _ := store.AddSpawn(AddSpawnOptions{Task: "x", ScheduleName: "d", CronExpr: "0 * * * *"})
	_, _ = store.RecordStartFailure(sc.ID, fmt.Errorf("%w (20) reached", ErrMaxSessions), rearmNow, DefaultRearmPolicy())
	again, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := again.Get(sc.ID)
	if got.RearmState != RearmStateWaiting || got.ConsecutiveWaits != 1 || got.LastError == "" || got.State != SchedPending {
		t.Fatalf("re-arm state lost across restart: %+v", got)
	}
}

func TestScheduleFireHistory_BoundedAndRecordsResults(t *testing.T) {
	store, sc := cronSched(t)
	for i := 0; i < ScheduleFireHistoryLimit+5; i++ {
		_ = store.RecordFire(sc.ID, fmt.Sprintf("testhost-s%d", i), "spawned")
	}
	_, _ = store.RecordStartFailure(sc.ID, ErrGlobalCooldown, rearmNow, DefaultRearmPolicy())
	got, _ := store.Get(sc.ID)
	if len(got.FireHistory) != ScheduleFireHistoryLimit {
		t.Fatalf("history must be bounded to %d, got %d", ScheduleFireHistoryLimit, len(got.FireHistory))
	}
	last := got.FireHistory[len(got.FireHistory)-1]
	if last.Result != "deferred" || last.Error == "" || last.At.IsZero() {
		t.Fatalf("newest history entry should be the deferred attempt with its reason: %+v", last)
	}
	if prev := got.FireHistory[len(got.FireHistory)-2]; prev.Result != "spawned" || prev.SessionID == "" {
		t.Fatalf("a spawned fire records its session id: %+v", prev)
	}
}

// The manager's schedule loop and the daemon's scheduler goroutine both fire
// due spawns. Whatever the caller, one due entry starts exactly one session.
func TestFireDueSessions_ConcurrentCallersStartOneSession(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	mgr.SetConfig(config.DefaultConfig())
	store, _ := NewScheduleStore(dataDir + "/schedule.json")
	mgr.SetScheduleStore(store)
	_, err = store.AddSpawn(AddSpawnOptions{Task: "true", ScheduleName: "once", Subprocess: true, OneShot: true, RunAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	before := len(mgr.store.List())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); mgr.FireDueSessions(context.Background(), time.Now()) }()
	}
	wg.Wait()
	if n := len(mgr.store.List()) - before; n != 1 {
		t.Fatalf("one due schedule must start exactly one session, started %d", n)
	}
	// Let the one-shot finish so TempDir cleanup finds no live writer.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		for _, s := range mgr.store.List() {
			if s.State != StateComplete && s.State != StateFailed {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFireDueSessions_AlertsOnWaitsAndOnGiveUp(t *testing.T) {
	mgr := newTestManager(t)
	mgr.SetConfig(config.DefaultConfig())
	store, _ := NewScheduleStore(t.TempDir() + "/schedule.json")
	mgr.SetScheduleStore(store)
	fillSessionCap(t, mgr, 1)
	var mu sync.Mutex
	var alerts []ScheduleAlert
	mgr.SetScheduleAlertHook(func(a ScheduleAlert) { mu.Lock(); alerts = append(alerts, a); mu.Unlock() })
	mgr.SetScheduleRearmPolicy(func() RearmPolicy { return RearmPolicy{Mode: RearmBackoff, MaxRetries: 1, Backoff: time.Second} })

	sc, _ := store.AddSpawn(AddSpawnOptions{Task: "true", ScheduleName: "detector", CronExpr: "*/5 * * * *", Subprocess: true, OneShot: true})
	for i := 0; i < ScheduleWaitAlertAfter; i++ {
		mgr.FireDueSessions(context.Background(), time.Now().Add(24*time.Hour))
		store.mu.Lock()
		sc.RunAt = time.Now().Add(-time.Second) // make it due again
		store.mu.Unlock()
	}
	mu.Lock()
	n := len(alerts)
	mu.Unlock()
	if n != 1 || !alerts[0].Outcome.Wait || alerts[0].Outcome.GaveUp {
		t.Fatalf("one capacity-wait alert expected after %d waits, got %+v", ScheduleWaitAlertAfter, alerts)
	}
	if got, _ := store.Get(sc.ID); got.State != SchedPending {
		t.Fatalf("waits leave the schedule pending, got %s", got.State)
	}

	// A real failure under the backoff policy gives up after one retry and alerts.
	_ = store.Cancel(sc.ID)
	mgr.SetMaxSessions(0)
	mgr.SetScheduleAlertHook(func(a ScheduleAlert) { mu.Lock(); alerts = append(alerts, a); mu.Unlock() })
	bad, _ := store.AddSpawn(AddSpawnOptions{Task: "true", ScheduleName: "bad", ProjectDir: "../../../../escape-the-workspace", RunAt: time.Now().Add(-time.Second)})
	for i := 0; i < 3; i++ {
		mgr.FireDueSessions(context.Background(), time.Now().Add(24*time.Hour))
		store.mu.Lock()
		if bad.State == SchedPending {
			bad.RunAt = time.Now().Add(-time.Second)
		}
		store.mu.Unlock()
	}
	got, _ := store.Get(bad.ID)
	if got.State != SchedFailed || got.RearmState != RearmStateGaveUp {
		t.Fatalf("policy must give up: %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if last := alerts[len(alerts)-1]; !last.Outcome.GaveUp || last.Schedule.ScheduleName != "bad" {
		t.Fatalf("an alert must be raised when the policy gives up: %+v", last)
	}
}

func TestRearmText_ShowsNextLastAndState(t *testing.T) {
	store, sc := cronSched(t)
	if got, _ := store.Get(sc.ID); !strings.HasPrefix(got.RearmText(), "next ") || strings.Contains(got.RearmText(), "last") {
		t.Fatalf("fresh schedule shows only the next fire: %q", got.RearmText())
	}
	_, _ = store.RecordStartFailure(sc.ID, fmt.Errorf("%w (20) reached", ErrMaxSessions), rearmNow, DefaultRearmPolicy())
	got, _ := store.Get(sc.ID)
	txt := got.RearmText()
	for _, want := range []string{"next ", "last deferred", "max sessions (20) reached", "waiting_capacity x1"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("%q missing %q", txt, want)
		}
	}
	_, _ = store.RecordStartFailure(sc.ID, errors.New("boom"), rearmNow, rearmPolicy(RearmStayFailed, 0))
	got, _ = store.Get(sc.ID)
	if txt := got.RearmText(); !strings.Contains(txt, "no next fire (failed)") || !strings.Contains(txt, "gave_up x1") {
		t.Fatalf("a schedule the policy gave up on must say so: %q", txt)
	}
}
