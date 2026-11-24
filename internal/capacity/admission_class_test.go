package capacity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func acq(t *testing.T, l *Ledger, req Request, wait time.Duration) error {
	t.Helper()
	return l.Acquire(context.Background(), req, wait, nil, nil)
}

// An operator start is admitted even when the pool is full. The lease records
// the overage (pool, who, how far over) and the pool reports it.
func TestOperatorClassAdmittedOverCapWithOverageLease(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:claude-code", 1)
	if err := acq(t, l, Request{Holder: "w1", Pools: []string{"llm:claude-code"}}, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"llm:claude-code"}}, 50*time.Millisecond); err != nil {
		t.Fatalf("operator start must never be refused: %v", err)
	}
	ls := l.LeaseOf("op1")
	if ls == nil || ls.Class != ClassOperator {
		t.Fatalf("lease = %+v, want operator class", ls)
	}
	if len(ls.Overage) != 1 || ls.Overage[0].Pool != "llm:claude-code" || ls.Overage[0].Limit != 1 || ls.Overage[0].Over != 1 {
		t.Fatalf("overage = %+v, want one record: pool llm:claude-code limit 1 over 1", ls.Overage)
	}
	st := l.Snapshot()
	for _, p := range st.Pools {
		if p.Name == "llm:claude-code" && (p.Held != 2 || p.Over != 1) {
			t.Fatalf("pool = %+v, want held 2 over 1", p)
		}
	}
}

// An operator lease inside the cap carries no overage record.
func TestOperatorClassWithinCapRecordsNoOverage(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:claude-code", 2)
	if err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"llm:claude-code"}}, time.Second); err != nil {
		t.Fatal(err)
	}
	if ls := l.LeaseOf("op1"); ls == nil || len(ls.Overage) != 0 {
		t.Fatalf("lease = %+v, want no overage", ls)
	}
}

// Autonomous work waits instead: a worker is blocked while an operator holds
// the pool over its cap, and the overage lease counts in the used figure.
func TestWorkerWaitsBehindOperatorOverage(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:claude-code", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"llm:claude-code"}}, time.Second)
	_ = acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"llm:claude-code"}}, time.Second)
	l.Release("w1")
	err := acq(t, l, Request{Holder: "w2", Pools: []string{"llm:claude-code"}}, 60*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("worker err = %v, want wait timeout while the operator holds the only slot", err)
	}
}

// A supervisor takes the reserved buffer on top of the cap, never queued
// behind workers, and a worker cannot use that buffer.
func TestSupervisorClassUsesReservedBuffer(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:claude-code", 1)
	l.SetReserve("llm:claude-code", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"llm:claude-code"}}, time.Second)
	if err := acq(t, l, Request{Holder: "w2", Pools: []string{"llm:claude-code"}}, 40*time.Millisecond); !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("second worker err = %v, want wait timeout: the buffer is not for workers", err)
	}
	if err := acq(t, l, Request{Holder: "s1", Class: ClassSupervisor, Pools: []string{"llm:claude-code"}}, 40*time.Millisecond); err != nil {
		t.Fatalf("supervisor must use the buffer slot: %v", err)
	}
	if err := acq(t, l, Request{Holder: "s2", Class: ClassSupervisor, Pools: []string{"llm:claude-code"}}, 40*time.Millisecond); !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("second supervisor err = %v, want wait timeout: the buffer holds one", err)
	}
	if ls := l.LeaseOf("s1"); ls == nil || ls.Class != ClassSupervisor || len(ls.Overage) != 0 {
		t.Fatalf("supervisor lease = %+v, want class supervisor and no overage", ls)
	}
}

// A queued supervisor is granted before a queued worker when a slot frees.
func TestSupervisorClassDispatchedBeforeWorker(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:pi", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"llm:pi"}}, time.Second)
	got := make(chan string, 2)
	go func() {
		if acq(t, l, Request{Holder: "w2", Pools: []string{"llm:pi"}}, 2*time.Second) == nil {
			got <- "w2"
		}
	}()
	time.Sleep(30 * time.Millisecond)
	go func() {
		if acq(t, l, Request{Holder: "s1", Class: ClassSupervisor, Pools: []string{"llm:pi"}}, 2*time.Second) == nil {
			got <- "s1"
		}
	}()
	time.Sleep(30 * time.Millisecond)
	l.Release("w1")
	select {
	case first := <-got:
		if first != "s1" {
			t.Fatalf("first grant = %s, want the supervisor", first)
		}
	case <-time.After(time.Second):
		t.Fatal("no grant after release")
	}
}

// SetReserve 0 clears the buffer and the reserve is readable.
func TestSetReserveClears(t *testing.T) {
	l := New(fast())
	l.SetReserve("host", 4)
	if l.Reserve("host") != 4 {
		t.Fatal("reserve not stored")
	}
	l.SetReserve("host", 0)
	if l.Reserve("host") != 0 {
		t.Fatal("reserve not cleared")
	}
}

// The overage notifier fires once per overage lease.
func TestOverageNotifierFires(t *testing.T) {
	got := make(chan Lease, 1)
	opt := fast()
	opt.OnOverage = func(ls Lease) { got <- ls }
	l := New(opt)
	l.SetLimit("llm:claude-code", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"llm:claude-code"}}, time.Second)
	_ = acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"llm:claude-code"}}, time.Second)
	select {
	case ls := <-got:
		if ls.Holder != "op1" || len(ls.Overage) != 1 {
			t.Fatalf("notified lease = %+v", ls)
		}
	case <-time.After(time.Second):
		t.Fatal("overage notifier did not fire")
	}
}

func TestFormatPoolsShowsOverageAndReserve(t *testing.T) {
	out := FormatPools([]PoolStatus{{Name: "llm:claude-code", Limit: 1, Held: 2, Over: 1, Reserve: 1}})
	for _, want := range []string{"2/1", "1 over the limit: operator", "+1 reserved for supervisors"} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatPools = %q, want %q in it", out, want)
		}
	}
}
