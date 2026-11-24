package capacity

import (
	"testing"
	"time"
)

// A restarted worker (Resume) that waits on a full pool is granted before an
// older waiting fresh worker when a slot frees, and it still obeys the cap.
func TestRestartedWorkerWaitsAtHeadAndStartsFirst(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:claude-code", 1)
	_ = acq(t, l, Request{Holder: "running", Pools: []string{"llm:claude-code"}}, time.Second)
	got := make(chan string, 2)
	go func() {
		if acq(t, l, Request{Holder: "fresh", PRDID: "p1", Priority: 90, Pools: []string{"llm:claude-code"}}, 3*time.Second) == nil {
			got <- "fresh"
		}
	}()
	time.Sleep(40 * time.Millisecond)
	go func() {
		if acq(t, l, Request{Holder: "restart", PRDID: "p2", Resume: true, Pools: []string{"llm:claude-code"}}, 3*time.Second) == nil {
			got <- "restart"
		}
	}()
	time.Sleep(40 * time.Millisecond)
	if l.Held("restart") {
		t.Fatal("a restarted worker must still respect the cap")
	}
	l.Release("running")
	select {
	case first := <-got:
		if first != "restart" {
			t.Fatalf("first grant = %s, want the restarted worker ahead of older fresh work", first)
		}
	case <-time.After(time.Second):
		t.Fatal("no grant after release")
	}
}

// A restarted worker does not outrank a verifier (rank) or take a supervisor's place.
func TestRestartedWorkerDoesNotOutrankSupervisor(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:pi", 1)
	_ = acq(t, l, Request{Holder: "running", Pools: []string{"llm:pi"}}, time.Second)
	got := make(chan string, 2)
	go func() {
		if acq(t, l, Request{Holder: "restart", Resume: true, Pools: []string{"llm:pi"}}, 3*time.Second) == nil {
			got <- "restart"
		}
	}()
	time.Sleep(40 * time.Millisecond)
	go func() {
		if acq(t, l, Request{Holder: "sup", Class: ClassSupervisor, Pools: []string{"llm:pi"}}, 3*time.Second) == nil {
			got <- "sup"
		}
	}()
	time.Sleep(40 * time.Millisecond)
	l.Release("running")
	if first := <-got; first != "sup" {
		t.Fatalf("first grant = %s, want the supervisor", first)
	}
}
