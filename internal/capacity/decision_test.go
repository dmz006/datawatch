package capacity

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// enqueue starts a blocking Acquire for req and waits until it is queued.
func enqueue(t *testing.T, l *Ledger, req Request) {
	t.Helper()
	go func() {
		_ = l.Acquire(context.Background(), req, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !queued(l, req.Holder) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never queued", req.Holder)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDecisionAdmitOnEmptyLedger(t *testing.T) {
	l := rateLedger(newFakeClock())
	l.SetLimit("p", 1)
	d := l.Check(Request{Holder: "a", Pools: []string{"p"}})
	if d.Kind != DecisionAdmit || d.QueuePosition != 0 || d.Reason != "" || d.ResetsAt != nil || len(d.Alternates) != 0 {
		t.Fatalf("decision = %+v", d)
	}
	if got := FormatDecision(d); !strings.Contains(got, "admitted") {
		t.Fatalf("format = %q", got)
	}
}

func TestDecisionQueuePositionMatchesGrantOrder(t *testing.T) {
	l := rateLedger(newFakeClock())
	l.SetLimit("p", 1)
	if err := tryAcquire(l, "seed", "p"); err != nil {
		t.Fatal(err)
	}
	enqueue(t, l, Request{Holder: "w1", PRDID: "a", Pools: []string{"p"}})
	enqueue(t, l, Request{Holder: "pl", PRDID: "b", Pools: []string{"p"}, Role: RolePlanner})
	enqueue(t, l, Request{Holder: "v", PRDID: "c", Pools: []string{"p"}, Role: RoleVerifier})
	enqueue(t, l, Request{Holder: "w4", PRDID: "d", Pools: []string{"p"}, Priority: 5})

	cases := []struct {
		req  Request
		want int
	}{
		{Request{Holder: "n-ver", PRDID: "z", Pools: []string{"p"}, Role: RoleVerifier, Priority: 1}, 1},
		{Request{Holder: "n-hi", PRDID: "z", Pools: []string{"p"}, Priority: 9}, 2},
		{Request{Holder: "n-wk", PRDID: "z", Pools: []string{"p"}}, 4},
		{Request{Holder: "n-pl", PRDID: "z", Pools: []string{"p"}, Role: RolePlanner}, 5},
	}
	for _, c := range cases {
		d := l.Check(c.req)
		if d.Kind != DecisionQueued || d.QueuePosition != c.want {
			t.Fatalf("%s: decision = %+v, want position %d", c.req.Holder, d, c.want)
		}
	}

	// The prediction for a plain worker must match the real grant order.
	enqueue(t, l, cases[2].req)
	var order []string
	all := []string{"w1", "pl", "v", "w4", "n-wk"}
	l.Release("seed")
	for range all {
		var cur string
		for _, h := range all {
			if l.Held(h) {
				cur = h
			}
		}
		if cur == "" {
			t.Fatal("nobody granted")
		}
		order = append(order, cur)
		l.Release(cur)
	}
	want := []string{"v", "w4", "w1", "n-wk", "pl"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("grant order = %v, want %v", order, want)
	}
}

func TestDecisionAlternatesListIdleElsewhere(t *testing.T) {
	l := rateLedger(newFakeClock())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetLimit("node:c", 1)
	l.SetLimit("llm:x", 1)
	l.SetLimit("llm:y", 1)
	for h, p := range map[string]string{"h1": "node:a", "h2": "node:c", "h3": "llm:x"} {
		if err := tryAcquire(l, h, p); err != nil {
			t.Fatal(err)
		}
	}
	d := l.Check(Request{Holder: "n", Pools: []string{"node:a"}, Node: "a", Model: "m1"})
	if d.Kind != DecisionAlternateAvailable || d.QueuePosition != 1 {
		t.Fatalf("decision = %+v", d)
	}
	want := []Alternate{{Pool: "node:b", Node: "b", Model: "m1"}}
	if !reflect.DeepEqual(d.Alternates, want) {
		t.Fatalf("alternates = %+v, want %+v", d.Alternates, want)
	}
	d = l.Check(Request{Holder: "n", Pools: []string{"llm:x"}})
	if d.Kind != DecisionAlternateAvailable || len(d.Alternates) != 1 || d.Alternates[0].Pool != "llm:y" {
		t.Fatalf("llm decision = %+v", d)
	}
	if got := FormatDecision(d); !strings.Contains(got, "alternate available: llm:y") || !strings.Contains(got, "position 1") {
		t.Fatalf("format = %q", got)
	}
	// A full pool with every sibling full has no alternates.
	if err := tryAcquire(l, "h4", "llm:y"); err != nil {
		t.Fatal(err)
	}
	d = l.Check(Request{Holder: "n", Pools: []string{"llm:x"}})
	if d.Kind != DecisionQueued || len(d.Alternates) != 0 {
		t.Fatalf("decision = %+v", d)
	}
}

func TestDecisionRateCeilingResetsAtAndAlternates(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetRate("llm:x", RateCeiling{Requests: 1, Window: time.Minute})
	start := clk.Now()
	if err := tryAcquire(l, "a", "llm:x"); err != nil {
		t.Fatal(err)
	}
	l.Release("a")
	d := l.Check(Request{Holder: "n", Pools: []string{"llm:x"}})
	if d.Kind != DecisionQueued || d.QueuePosition != 1 || d.ResetsAt == nil || !d.ResetsAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("decision = %+v", d)
	}
	if !strings.Contains(d.Reason, "rate ceiling") || !strings.Contains(FormatDecision(d), "resets at") {
		t.Fatalf("reason/format = %q / %q", d.Reason, FormatDecision(d))
	}
	// A sibling pool with room becomes an alternate; one that is itself
	// rate-blocked does not.
	l.SetLimit("llm:y", 1)
	l.SetRate("llm:z", RateCeiling{Requests: 1, Window: time.Minute})
	if err := tryAcquire(l, "b", "llm:z"); err != nil {
		t.Fatal(err)
	}
	l.Release("b")
	d = l.Check(Request{Holder: "n", Pools: []string{"llm:x"}})
	if d.Kind != DecisionAlternateAvailable || len(d.Alternates) != 1 || d.Alternates[0].Pool != "llm:y" || d.ResetsAt == nil {
		t.Fatalf("decision = %+v", d)
	}
	clk.Advance(time.Minute)
	if d := l.Check(Request{Holder: "n", Pools: []string{"llm:x"}}); d.Kind != DecisionAdmit {
		t.Fatalf("after window: %+v", d)
	}
}

func TestDecisionNoLeaseNoMutation(t *testing.T) {
	clk := newFakeClock()
	l := rateLedger(clk)
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetRate("node:a", RateCeiling{Requests: 5, Window: time.Minute})
	if err := tryAcquire(l, "seed", "node:a"); err != nil {
		t.Fatal(err)
	}
	enqueue(t, l, Request{Holder: "w", PRDID: "p1", Pools: []string{"node:a"}, Node: "a", Model: "m"})
	before := l.Snapshot()
	l.mu.Lock()
	grants, models, seq := len(l.lastGrant), len(l.lastModel), l.seq
	l.mu.Unlock()
	for i := 0; i < 50; i++ {
		l.Check(Request{Holder: "n", PRDID: "p2", Pools: []string{"node:a"}, Node: "a", Model: "m2"})
		l.Check(Request{Holder: "free", PRDID: "p3", Pools: []string{"node:b"}, Node: "b", Model: "m3"})
	}
	if l.Held("n") || l.Held("free") {
		t.Fatal("Check must take no lease")
	}
	if after := l.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("snapshot changed:\nbefore %+v\nafter  %+v", before, after)
	}
	l.mu.Lock()
	if len(l.lastGrant) != grants || len(l.lastModel) != models || l.seq != seq {
		t.Fatal("Check mutated grant/model/seq state")
	}
	l.mu.Unlock()

	// A following Acquire behaves as if Check never ran.
	if err := l.Acquire(context.Background(), Request{Holder: "free", PRDID: "p3", Pools: []string{"node:b"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(l.Snapshot().Waiting); n != 1 {
		t.Fatalf("waiting = %d, want 1", n)
	}
	l.Release("seed")
	deadline := time.Now().Add(2 * time.Second)
	for !l.Held("w") {
		if time.Now().After(deadline) {
			t.Fatal("w never granted")
		}
		time.Sleep(time.Millisecond)
	}
}
