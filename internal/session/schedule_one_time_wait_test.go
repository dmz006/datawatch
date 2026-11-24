package session

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var errCapacityRefusal = fmt.Errorf("%w (20) reached", ErrMaxSessions)

func oneTimePolicy(maxWait time.Duration) RearmPolicy {
	return RearmPolicy{Mode: RearmNextFire, Backoff: 30 * time.Second, BackoffMax: 15 * time.Minute, OneTimeMaxWait: maxWait}
}

// A one-time schedule refused for capacity keeps waiting until the limit and
// gives up at it, with the reason on the entry. The clock is the now argument.
func TestOneTimeSchedule_WaitsUntilLimitThenGivesUp(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", ScheduleName: "once", RunAt: rearmNow.Add(-time.Minute)})
	p := oneTimePolicy(12 * time.Hour)
	now := rearmNow
	var out StartFailure
	// Retry every time it is due, as the scheduler does, up to one minute before the limit.
	for now.Before(rearmNow.Add(12*time.Hour - time.Minute)) {
		var err error
		out, err = store.RecordStartFailure(sc.ID, errCapacityRefusal, now, p)
		if err != nil {
			t.Fatal(err)
		}
		if out.GaveUp {
			t.Fatalf("gave up too early at %v (waited %v)", now, now.Sub(rearmNow))
		}
		got, _ := store.Get(sc.ID)
		now = got.RunAt
	}
	got, _ := store.Get(sc.ID)
	if got.State != SchedPending || got.RearmState != RearmStateWaiting {
		t.Fatalf("still inside the limit the schedule must be pending and waiting: %+v", got)
	}
	// The next retry must be no later than the limit itself.
	if limit := rearmNow.Add(12 * time.Hour); got.RunAt.After(limit) {
		t.Fatalf("the last retry before the limit must not be scheduled past it: run_at %v limit %v", got.RunAt, limit)
	}
	now = got.RunAt
	out, err := store.RecordStartFailure(sc.ID, errCapacityRefusal, now, p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(sc.ID)
	if !out.GaveUp || !out.WaitLimit || out.Rearmed || out.Wait {
		t.Fatalf("at the limit the outcome must be a wait-limit give-up: %+v", out)
	}
	if got.State != SchedFailed || got.RearmState != RearmStateGaveUp {
		t.Fatalf("a one-time schedule past the limit must fail with rearm_state gave_up: %+v", got)
	}
	for _, want := range []string{"12 hours", "max sessions (20) reached"} {
		if !strings.Contains(got.LastError, want) {
			t.Fatalf("last_error must give the reason, missing %q: %q", want, got.LastError)
		}
	}
	if got.LastFireResult != "failed" {
		t.Fatalf("last result must be failed, got %q", got.LastFireResult)
	}
	last := got.FireHistory[len(got.FireHistory)-1]
	if last.Result != "failed" || !strings.Contains(last.Error, "12 hours") {
		t.Fatalf("history must record the give-up with its reason: %+v", last)
	}
}

func TestOneTimeSchedule_LimitIsTheSetting(t *testing.T) {
	for _, hours := range []int{1, 3} {
		store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
		p := oneTimePolicy(time.Duration(hours) * time.Hour)
		_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow, p)
		before, _ := store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow.Add(time.Duration(hours)*time.Hour-time.Second), p)
		if before.GaveUp {
			t.Fatalf("%dh limit: gave up one second early", hours)
		}
		at, _ := store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow.Add(time.Duration(hours)*time.Hour), p)
		if !at.GaveUp || !at.WaitLimit {
			t.Fatalf("%dh limit: must give up at the limit: %+v", hours, at)
		}
		if got, _ := store.Get(sc.ID); !strings.Contains(got.LastError, fmt.Sprintf("%d hour", hours)) {
			t.Fatalf("reason must name the %dh limit: %q", hours, got.LastError)
		}
	}
}

// Recurring schedules keep waiting without a limit, as before.
func TestRecurringSchedule_NeverGivesUpWaiting(t *testing.T) {
	store, sc := cronSched(t)
	p := oneTimePolicy(time.Hour)
	for i := 0; i < 100; i++ {
		out, err := store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow.Add(time.Duration(i)*time.Hour), p)
		if err != nil || out.GaveUp || out.WaitLimit || !out.Rearmed {
			t.Fatalf("a recurring schedule must keep waiting past the one-time limit (i=%d): %+v err=%v", i, out, err)
		}
	}
	if got, _ := store.Get(sc.ID); got.State != SchedPending {
		t.Fatalf("recurring schedule must stay pending: %+v", got)
	}
}

// The wait clock restarts after a successful start, and a real failure is not
// a wait: it is handled by the re-arm mode as before.
func TestOneTimeSchedule_WaitClockResetsOnSuccess(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
	p := oneTimePolicy(2 * time.Hour)
	_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow, p)
	if got, _ := store.Get(sc.ID); got.WaitingSince.IsZero() {
		t.Fatal("the first wait must record when waiting began")
	}
	if err := store.RecordFire(sc.ID, "testhost-s1", "spawned"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(sc.ID); !got.WaitingSince.IsZero() {
		t.Fatalf("a spawned fire must clear the wait clock: %v", got.WaitingSince)
	}
	// A new run of waits starts a new clock: 90 minutes after the old start is fine.
	late := rearmNow.Add(90 * time.Minute)
	_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, late, p)
	out, _ := store.RecordStartFailure(sc.ID, errCapacityRefusal, late.Add(time.Hour), p)
	if out.GaveUp {
		t.Fatalf("clock must restart after a success, gave up after only an hour: %+v", out)
	}
}

func TestOneTimeSchedule_RealFailureStillFailsAtOnce(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
	out, _ := store.RecordStartFailure(sc.ID, errors.New("tmux exploded"), rearmNow, oneTimePolicy(12*time.Hour))
	got, _ := store.Get(sc.ID)
	if !out.GaveUp || out.WaitLimit || got.State != SchedFailed {
		t.Fatalf("a real failure of a one-time schedule is unchanged: %+v out=%+v", got, out)
	}
}

func TestOneTimeSchedule_WaitClockSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/schedule.json"
	store, _ := NewScheduleStore(path)
	sc, _ := store.AddSpawn(AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
	p := oneTimePolicy(2 * time.Hour)
	_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow, p)
	again, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := again.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow.Add(2*time.Hour), p)
	if !out.GaveUp || !out.WaitLimit {
		t.Fatalf("the wait clock must survive a daemon restart: %+v", out)
	}
}

func TestRearmPolicy_OneTimeMaxWaitDefaultsToTwelveHours(t *testing.T) {
	if got := DefaultRearmPolicy().OneTimeMaxWait; got != 12*time.Hour {
		t.Fatalf("default one-time wait limit must be 12h, got %v", got)
	}
	if got := (RearmPolicy{OneTimeMaxWait: -time.Hour}).Normalised().OneTimeMaxWait; got != 12*time.Hour {
		t.Fatalf("an unusable limit repairs to 12h, got %v", got)
	}
	if got := (RearmPolicy{OneTimeMaxWait: 3 * time.Hour}).Normalised().OneTimeMaxWait; got != 3*time.Hour {
		t.Fatalf("a set limit is kept, got %v", got)
	}
}

// A wait-limit give-up is not a run of failed starts, so the one-line state
// must not say "gave_up x0".
func TestRearmText_WaitLimitGiveUpSaysItWaited(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: rearmNow.Add(-time.Minute)})
	p := oneTimePolicy(time.Hour)
	_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow, p)
	_, _ = store.RecordStartFailure(sc.ID, errCapacityRefusal, rearmNow.Add(time.Hour), p)
	got, _ := store.Get(sc.ID)
	txt := got.RearmText()
	for _, want := range []string{"no next fire (failed)", "gave_up (waited 2 tries)", "gave up waiting for capacity after 1 hour"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("%q missing %q", txt, want)
		}
	}
	if strings.Contains(txt, "x0") {
		t.Fatalf("must not claim zero failures: %q", txt)
	}
}
