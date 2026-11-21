package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// judgeTimerClock is a clock a test advances by hand, standing in for the
// wall clock of the capacity lane.
type judgeTimerClock struct{ now time.Time }

func (c *judgeTimerClock) Now() time.Time      { return c.now }
func (c *judgeTimerClock) Add(d time.Duration) { c.now = c.now.Add(d) }

// TestJudgeRunTimerLimitStartsWhenTheJudgeStarts: a judge queued behind a
// full lane waits 60 minutes for a slot, then runs for 5 minutes and gives
// its verdict. The 60-minute wait is recorded as waiting and never counts
// against the 30-minute limit, which starts when the run starts.
func TestJudgeRunTimerLimitStartsWhenTheJudgeStarts(t *testing.T) {
	clk := &judgeTimerClock{now: time.Unix(1_000_000, 0)}
	rt := &JudgeRunTimer{Clock: clk.Now, Limit: 30 * time.Minute}

	rt.BeginWait()            // the lane is full; the judge waits in the queue
	clk.Add(60 * time.Minute) // the lane stays full the whole time
	if rt.Expired() {
		t.Fatal("a judge that is still waiting for a slot must never be expired")
	}
	if e := rt.Elapsed(); e != 0 {
		t.Fatalf("elapsed while waiting = %s, want 0: the run has not started", e)
	}
	if w := rt.Waiting(); w != 60*time.Minute {
		t.Fatalf("waiting = %s, want 60m", w)
	}

	rt.Grant()               // the slot is granted: the run — and the limit — start here
	clk.Add(5 * time.Minute) // the judge runs 5 minutes and gives its verdict
	if rt.Expired() {
		t.Fatal("a 5 minute run is inside the 30 minute limit; the 60 minute wait must not count against it")
	}
	if w := rt.Waiting(); w != 60*time.Minute {
		t.Fatalf("waiting = %s, want 60m", w)
	}
	if e := rt.Elapsed(); e != 5*time.Minute {
		t.Fatalf("elapsed = %s, want 5m from when the run started", e)
	}
}

// TestJudgeRunTimerRunPastTheLimitIsEnded: the lane is free, so the judge
// starts almost at once and runs 40 minutes against the 30-minute limit. It
// is ended at the limit and recorded as timed out, with the waiting and the
// running recorded separately on the attempt record.
func TestJudgeRunTimerRunPastTheLimitIsEnded(t *testing.T) {
	clk := &judgeTimerClock{now: time.Unix(2_000_000, 0)}
	rt := &JudgeRunTimer{Clock: clk.Now, Limit: 30 * time.Minute}

	rt.BeginWait()
	clk.Add(10 * time.Second) // a short wait; the lane grants the slot
	rt.Grant()
	if rt.Expired() {
		t.Fatal("a run that just started must not be expired")
	}
	if w := rt.Waiting(); w != 10*time.Second {
		t.Fatalf("waiting = %s, want the 10s wait", w)
	}
	clk.Add(30 * time.Minute) // the run reaches its limit
	if !rt.Expired() {
		t.Fatal("the run reached its 30 minute limit and must be ended")
	}

	// Recorded as timed out, waiting and running kept separate.
	rec := JudgeAttemptRecord{
		Backend:  "claude-code",
		Started:  clk.now.Add(-40*time.Minute - 10*time.Second),
		Duration: rt.Elapsed().Round(time.Second),
		Waiting:  rt.Waiting().Round(time.Second),
		Outcome:  JudgeOutcomeTimedOut,
	}
	if rec.Duration != 30*time.Minute {
		t.Fatalf("duration = %s, want the 30 minute run", rec.Duration)
	}
	if rec.Waiting != 10*time.Second {
		t.Fatalf("waiting = %s, want the 10 second wait", rec.Waiting)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back JudgeAttemptRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Duration != rec.Duration || back.Waiting != rec.Waiting {
		t.Fatalf("round trip = duration %s waiting %s, want %s and %s", back.Duration, back.Waiting, rec.Duration, rec.Waiting)
	}
	s := string(raw)
	if !strings.Contains(s, `"duration"`) || !strings.Contains(s, `"waiting"`) {
		t.Fatalf("record %s must keep both durations", s)
	}
}

// TestJudgeRunTimerWaitAccumulatesAcrossRetargets: the judge leaves a full
// lane for another backend and waits there too. Both waits count as waiting,
// and the run — with the limit — starts only when the second slot is
// granted.
func TestJudgeRunTimerWaitAccumulatesAcrossRetargets(t *testing.T) {
	clk := &judgeTimerClock{now: time.Unix(3_000_000, 0)}
	rt := &JudgeRunTimer{Clock: clk.Now, Limit: 30 * time.Minute}

	rt.BeginWait() // the first lane is full
	clk.Add(20 * time.Minute)
	rt.BeginWait() // the judge moves to the next backend and waits there
	clk.Add(40 * time.Minute)
	rt.Grant()

	if w := rt.Waiting(); w != 60*time.Minute {
		t.Fatalf("waiting = %s, want both waits (60m)", w)
	}
	if e := rt.Elapsed(); e != 0 {
		t.Fatalf("elapsed = %s, want 0: the run has not started", e)
	}
	clk.Add(1 * time.Minute)
	if e := rt.Elapsed(); e != 1*time.Minute {
		t.Fatalf("elapsed = %s, want 1m from the second grant", e)
	}
	if rt.Expired() {
		t.Fatal("1 minute into the run is inside the limit; the 60 minute wait must not count against it")
	}
}

// TestJudgeRunTimerNoLimitNeverExpires: a judge whose run has no limit is
// never ended by the timer, however long it runs.
func TestJudgeRunTimerNoLimitNeverExpires(t *testing.T) {
	clk := &judgeTimerClock{now: time.Unix(4_000_000, 0)}
	rt := &JudgeRunTimer{Clock: clk.Now}
	rt.Grant()
	clk.Add(time.Hour)
	if rt.Expired() {
		t.Fatal("a judge whose run has no limit must never expire")
	}
	if e := rt.Elapsed(); e != time.Hour {
		t.Fatalf("elapsed = %s, want 1h", e)
	}
}

// TestJudgeRunTimerWaitingWithoutAGrantStaysWaiting: a judge that never got
// its slot has all of its time as waiting and no run at all.
func TestJudgeRunTimerWaitingWithoutAGrantStaysWaiting(t *testing.T) {
	clk := &judgeTimerClock{now: time.Unix(5_000_000, 0)}
	rt := &JudgeRunTimer{Clock: clk.Now, Limit: 30 * time.Minute}
	rt.BeginWait()
	clk.Add(45 * time.Minute) // the lane stays full past the limit
	if rt.Expired() {
		t.Fatal("a judge that never got its slot must never be expired by the run limit")
	}
	if e := rt.Elapsed(); e != 0 {
		t.Fatalf("elapsed = %s, want 0: the run never started", e)
	}
	if w := rt.Waiting(); w != 45*time.Minute {
		t.Fatalf("waiting = %s, want 45m", w)
	}
	rec := JudgeAttemptRecord{
		Backend:  "claude-code",
		Started:  clk.now.Add(-45 * time.Minute),
		Duration: rt.Elapsed(),
		Waiting:  rt.Waiting(),
		Outcome:  JudgeOutcomeNoSlot,
	}
	if rec.Duration != 0 {
		t.Fatalf("duration = %s, want 0: the judge never ran", rec.Duration)
	}
	if rec.Waiting != 45*time.Minute {
		t.Fatalf("waiting = %s, want 45m", rec.Waiting)
	}
}
