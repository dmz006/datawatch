package capacity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The review reserve, seen from the capacity ledger: the gate is installed
// through Options.RoleBackpressure exactly the way the daemon wires its
// back-off-with-reserve gate, and the deterministic fake clock in
// fake_clock_test.go drives every deadline, so no test below sleeps or reads
// the wall clock.

// reserveWaitReason is the gate's refusal sub-reason (the autonomous
// package's BackoffReserveWaitReason constant, kept local here so the
// capacity package never imports the daemon on top of it).
const reserveWaitReason = "back-off, review reserve in use"

// reviewGate is a backed-off pool that admits review roles (verifier,
// planner) only up to maxSlots live review leases and refuses workers
// outright; maxSlots 0 means the reserve is off and every role is refused.
func reviewGate(maxSlots int) func(pool string, role Role, heldByRole map[Role]int) (bool, string) {
	return func(pool string, role Role, heldByRole map[Role]int) (bool, string) {
		review := heldByRole[RoleVerifier] + heldByRole[RolePlanner]
		switch {
		case role == RoleWorker:
			return true, reserveWaitReason
		case review >= maxSlots:
			return true, reserveWaitReason
		}
		return false, ""
	}
}

// reserveTestEnv is the real in-memory ledger with the deterministic fake
// clock and a long poll interval, so only clk.Advance ever moves time.
type reserveTestEnv struct {
	l   *Ledger
	clk *fakeClock
}

func newReserveLedger(gate func(pool string, role Role, heldByRole map[Role]int) (bool, string)) *reserveTestEnv {
	clk := newFakeClock()
	return &reserveTestEnv{l: New(Options{PollInterval: time.Hour, Clock: clk, RoleBackpressure: gate}), clk: clk}
}

// queuedReason runs req in a goroutine and returns (deterministically, via
// the onWait callback) the wait reason the ledger reported once it was
// blocked, plus a channel on which its final acquire error will arrive.
// The request must be blocked; if it is granted instead the test fails.
func queuedReason(t *testing.T, l *Ledger, req Request) (reason string, done <-chan error) {
	t.Helper()
	waited := make(chan string, 1)
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- l.Acquire(context.Background(), req, time.Minute, nil, func(r string) {
			select {
			case waited <- r:
			default:
			}
		})
	}()
	select {
	case reason = <-waited:
	case err := <-doneCh:
		t.Fatalf("%s/%s request was granted although it should have been blocked: %v", req.PRDID, req.Role, err)
	}
	return reason, doneCh
}

// a) The backend is backed off (the gate is installed): the guardrail
// verifier and the planning planner are each admitted through the review
// reserve's slots even though the pool is shared, and the whole worker queue
// is refused behind them.
func TestReserveAdmitsVerifierWhileBackedOff(t *testing.T) {
	env := newReserveLedger(reviewGate(2))
	l, clk := env.l, env.clk
	l.SetLimit("llm:cc", 4) // room to spare: the refusal must come from the gate
	ctx := context.Background()

	if err := l.Acquire(ctx, Request{Holder: "v", PRDID: "V", Pools: []string{"llm:cc"}, Role: RoleVerifier}, time.Second, nil, nil); err != nil {
		t.Fatalf("verifier should be admitted through the review reserve: %v", err)
	}
	if err := l.Acquire(ctx, Request{Holder: "p", PRDID: "P", Pools: []string{"llm:cc"}, Role: RolePlanner}, time.Second, nil, nil); err != nil {
		t.Fatalf("planner should be admitted through the review reserve: %v", err)
	}
	if !l.Held("v") || !l.Held("p") {
		t.Fatal("verifier and planner should hold their reserve slots")
	}

	// The worker is refused by the gate on the same pool and times out.
	wReason, wDone := queuedReason(t, l, Request{Holder: "w", PRDID: "W", Pools: []string{"llm:cc"}, Role: RoleWorker})
	if wReason != "pool llm:cc backpressure: "+reserveWaitReason {
		t.Fatalf("worker wait reason = %q, want the reserve refusal", wReason)
	}
	clk.Advance(2 * time.Minute)
	if err := <-wDone; !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("worker should time out still refused, got %v", err)
	}
}

// b) + e) While the backend is backed off the worker is refused with the
// reserve's wait reason, and a second review role finds the reserve full
// and reads the same text.
func TestReserveRefusalReasonForWorkerAndFullReserve(t *testing.T) {
	env := newReserveLedger(reviewGate(1))
	l, clk := env.l, env.clk
	l.SetLimit("llm:cc", 4)
	if err := l.Acquire(context.Background(), Request{Holder: "v", PRDID: "V", Pools: []string{"llm:cc"}, Role: RoleVerifier}, time.Second, nil, nil); err != nil {
		t.Fatalf("first review session should take the reserve: %v", err)
	}

	want := "pool llm:cc backpressure: " + reserveWaitReason
	wReason, wDone := queuedReason(t, l, Request{Holder: "w", PRDID: "W", Pools: []string{"llm:cc"}, Role: RoleWorker})
	if wReason != want {
		t.Fatalf("worker wait reason = %q, want %q", wReason, want)
	}
	clk.Advance(2 * time.Minute)
	err := <-wDone
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("worker should time out still refused, got %v", err)
	}
	if !strings.Contains(err.Error(), reserveWaitReason) {
		t.Fatalf("timeout error %v should carry the reserve wait reason", err)
	}

	pReason, pDone := queuedReason(t, l, Request{Holder: "p", PRDID: "P", Pools: []string{"llm:cc"}, Role: RolePlanner})
	if pReason != want {
		t.Fatalf("planner wait reason = %q, want %q", pReason, want)
	}
	clk.Advance(2 * time.Minute)
	if err := <-pDone; !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("planner should time out still refused, got %v", err)
	}
}

// c) reserve_for_review 0 refuses both worker and verifier, but never an
// operator (interactive) session.
func TestReserveZeroRefusesWorkerAndVerifier(t *testing.T) {
	env := newReserveLedger(reviewGate(0))
	l, clk := env.l, env.clk
	l.SetLimit("llm:cc", 2)
	ctx := context.Background()

	want := "pool llm:cc backpressure: " + reserveWaitReason
	for _, req := range []Request{
		{Holder: "w", PRDID: "W", Pools: []string{"llm:cc"}, Role: RoleWorker},
		{Holder: "v", PRDID: "V", Pools: []string{"llm:cc"}, Role: RoleVerifier},
	} {
		reason, _ := queuedReason(t, l, req)
		if reason != want {
			t.Fatalf("%s wait reason = %q, want %q", req.Role, reason, want)
		}
	}
	clk.Advance(2 * time.Minute) // let both refused waiters time out

	// Operator sessions skip the reserve entirely and still get a slot.
	if err := l.Acquire(ctx, Request{Holder: "op", PRDID: "O", Pools: []string{"llm:cc"}, Interactive: true}, time.Second, nil, nil); err != nil {
		t.Fatalf("operator session must not be held back by the reserve: %v", err)
	}
}

// d) Even where the reserve would admit it (the gate here admits every
// role), the verifier must not take a pool's last free slot out from under a
// worker queued for that pool: the ledger's last-slot rule holds the slot
// for the worker, and the worker wins it when the holder drains.
func TestReserveVerifierDoesNotTakeQueuedWorkersLastSlot(t *testing.T) {
	env := newReserveLedger(func(pool string, role Role, heldByRole map[Role]int) (bool, string) {
		return false, ""
	})
	l, clk := env.l, env.clk
	l.SetLimit("llm:b", 1)  // held by h: this is what blocks the worker
	l.SetLimit("node:a", 2) // one free slot — the "last slot"
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "h", PRDID: "H", Pools: []string{"llm:b", "node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("worker h should take its slots: %v", err)
	}

	// w queues behind the full llm:b, at a long fake deadline so it outlives
	// the verifier's wait.
	wWaited := make(chan string, 1)
	wDone := make(chan error, 1)
	go func() {
		wDone <- l.Acquire(ctx, Request{Holder: "w", PRDID: "W", Pools: []string{"llm:b", "node:a"}, Role: RoleWorker}, 10*time.Minute, nil, func(r string) {
			select {
			case wWaited <- r:
			default:
			}
		})
	}()
	if r := <-wWaited; !strings.Contains(r, "pool llm:b full") {
		t.Fatalf("worker wait reason = %q, want the full llm:b pool", r)
	}

	// The reserve is open (the gate admits it) but the last-slot rule still
	// holds node:a's last free slot for w.
	vReasonCh := make(chan string, 1)
	vDone := make(chan error, 1)
	go func() {
		vDone <- l.Acquire(ctx, Request{Holder: "v", PRDID: "V", Pools: []string{"node:a"}, Role: RoleVerifier}, time.Minute, nil, func(r string) {
			select {
			case vReasonCh <- r:
			default:
			}
		})
	}()
	if r := <-vReasonCh; !strings.Contains(r, "last slot held for queued worker(s) w") {
		t.Fatalf("verifier wait reason = %q, want the last-slot rule naming w", r)
	}

	// The verifier's deadline comes first: it gives up and the slot is
	// still held back for the queued worker.
	clk.Advance(90 * time.Second)
	if err := <-vDone; !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("verifier should time out on its fake deadline, got %v", err)
	}
	if l.Held("v") {
		t.Fatal("verifier holds a slot although it was refused and timed out")
	}

	// h drains: the slot goes to the queued worker, not back to review work.
	l.Release("h")
	select {
	case err := <-wDone:
		if err != nil {
			t.Fatalf("worker should be granted once h drains: %v", err)
		}
	case r := <-vReasonCh:
		t.Fatalf("unexpected reason event: %s", r)
	}
	if !l.Held("w") {
		t.Fatal("worker should hold the last slot it won")
	}
}
