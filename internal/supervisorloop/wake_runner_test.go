package supervisorloop

import (
	"sync/atomic"
	"testing"
)

func runnerBatch(ids ...string) WakeBatch {
	var b WakeBatch
	for _, id := range ids {
		b.Events = append(b.Events, WakeEvent{Kind: "k", AutomatonID: id})
	}
	return b
}

func TestWakeSingleFlightNoOverlap(t *testing.T) {
	started := make(chan WakeBatch)
	release := make(chan struct{})
	var active, maxActive int32
	r := NewRunner(func(b WakeBatch) {
		n := atomic.AddInt32(&active, 1)
		if n > atomic.LoadInt32(&maxActive) {
			atomic.StoreInt32(&maxActive, n)
		}
		started <- b
		<-release
		atomic.AddInt32(&active, -1)
	})

	if !r.Submit(runnerBatch("a")) {
		t.Fatal("first submit should start a run")
	}
	<-started
	for _, id := range []string{"b", "c", "d"} {
		if r.Submit(runnerBatch(id)) {
			t.Fatalf("submit %s started a concurrent run", id)
		}
	}
	release <- struct{}{} // finish first run
	<-started             // follow-up begins
	release <- struct{}{}
	r.Wait()
	if m := atomic.LoadInt32(&maxActive); m != 1 {
		t.Fatalf("max concurrent runs = %d, want 1", m)
	}
	if r.Running() {
		t.Fatal("runner still running after Wait")
	}
}

func TestWakeSingleFlightOneFollowUp(t *testing.T) {
	started := make(chan WakeBatch)
	release := make(chan struct{})
	var runs int32
	r := NewRunner(func(b WakeBatch) {
		atomic.AddInt32(&runs, 1)
		started <- b
		<-release
	})

	r.Submit(runnerBatch("a"))
	<-started
	r.Submit(runnerBatch("b", "c"))
	r.Submit(runnerBatch("c", "d")) // c is a duplicate of a queued condition
	release <- struct{}{}
	follow := <-started
	if got := len(follow.Events); got != 3 {
		t.Fatalf("follow-up has %d events, want 3 merged distinct", got)
	}
	release <- struct{}{}
	r.Wait()
	if n := atomic.LoadInt32(&runs); n != 2 {
		t.Fatalf("runs = %d, want 2 (initial plus one follow-up)", n)
	}
}
