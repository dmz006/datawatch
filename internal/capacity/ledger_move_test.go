package capacity

import (
	"context"
	"testing"
	"time"
)

func TestMove_WaitsForFullTargetAndKeepsOldLease(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:pi", 1)
	l.SetLimit("llm:claude", 2)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "other", Pools: []string{"llm:pi"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(ctx, Request{Holder: "t1", Pools: []string{"llm:claude"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	l.Bind("t1", "sess")
	done := make(chan error, 1)
	go func() {
		done <- l.Move(ctx, Request{Holder: "t1", Pools: []string{"llm:pi"}}, 5*time.Second, nil, nil)
	}()
	for i := 0; i < 200 && l.waitersCount("t1"+moveSuffix) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if l.waitersCount("t1"+moveSuffix) != 1 {
		t.Fatal("move never queued")
	}
	if ls := l.LeaseOf("t1"); ls == nil || ls.Pools[0] != "llm:claude" {
		t.Fatalf("old lease must stay while the target is full: %+v", ls)
	}
	l.Release("other")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ls := l.LeaseOf("t1")
	if ls == nil || ls.Pools[0] != "llm:pi" || ls.SessionID != "sess" {
		t.Fatalf("lease after move: %+v", ls)
	}
	if l.Held("t1" + moveSuffix) {
		t.Fatal("temporary key left behind")
	}
}

func TestMove_ImmediateWhenRoom(t *testing.T) {
	l := New(fast())
	l.SetLimit("llm:pi", 1)
	ctx := context.Background()
	_ = l.Acquire(ctx, Request{Holder: "t1", Pools: []string{"llm:claude"}}, time.Second, nil, nil)
	if err := l.Move(ctx, Request{Holder: "t1", Pools: []string{"llm:pi"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ls := l.LeaseOf("t1"); ls == nil || ls.Pools[0] != "llm:pi" {
		t.Fatalf("%+v", ls)
	}
}
