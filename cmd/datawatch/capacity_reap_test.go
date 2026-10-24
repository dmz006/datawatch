package main

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

func reapReq(holder string) capacity.Request {
	return capacity.Request{Holder: holder, PRDID: "p", Pools: []string{"gpu"}}
}

// reapPass runs one reaper pass at the given fake time.
func reapPass(led *capacity.Ledger, reg *verifierRegistry, alive map[string]bool, now time.Time) []string {
	return led.Reap(func(ls capacity.Lease) bool {
		return leaseHolderLive(ls, reg, reg.isActive, func(id string) bool { return alive[id] }, now)
	})
}

func TestLeaseReaperKeepsLongRunningVerifier(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	reg := newVerifierRegistry()
	holder := autonomous.VerifyHolder("t1")
	led.Adopt(reapReq(holder), "")
	reg.begin(holder)
	start := time.Now()
	for _, d := range []time.Duration{time.Minute, 11 * time.Minute, 25 * time.Minute, 29 * time.Minute} {
		if got := reapPass(led, reg, nil, start.Add(d)); len(got) != 0 {
			t.Fatalf("reaped %v at +%v", got, d)
		}
	}
	reg.bindSession(holder, "v1")
	led.Bind(holder, "v1")
	if got := reapPass(led, reg, map[string]bool{"v1": true}, start.Add(30*time.Minute)); len(got) != 0 {
		t.Fatalf("reaped %v with live verifier session", got)
	}
	if !led.Held(holder) {
		t.Fatal("lease lost")
	}
}

func TestLeaseReaperKeepsHandedOffLeaseWhenWorkerFinished(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	reg := newVerifierRegistry()
	led.Adopt(reapReq("t2"), "worker")
	if !led.Handoff("t2", autonomous.VerifyHolder("t2")) {
		t.Fatal("handoff")
	}
	holder := autonomous.VerifyHolder("t2")
	reg.begin(holder)
	// worker session is complete (absent from alive) and no verifier session yet
	if got := reapPass(led, reg, nil, time.Now()); len(got) != 0 {
		t.Fatalf("reaped %v", got)
	}
	reg.bindSession(holder, "v2")
	led.Bind(holder, "v2")
	if got := reapPass(led, reg, map[string]bool{"v2": true, "worker": false}, time.Now()); len(got) != 0 {
		t.Fatalf("reaped %v", got)
	}
}

func TestLeaseReaperReapsLeaseWhoseVerifierSessionCompleted(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	reg := newVerifierRegistry()
	holder := autonomous.VerifyHolder("t3")
	led.Adopt(reapReq(holder), "worker")
	reg.begin(holder)
	reg.bindSession(holder, "v3")
	led.Bind(holder, "v3")
	got := reapPass(led, reg, map[string]bool{"v3": false}, time.Now())
	if len(got) != 1 || got[0] != holder {
		t.Fatalf("reaped %v, want [%s]", got, holder)
	}
}

func TestLeaseReaperReapsLeakedUnregisteredLeases(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 2)
	reg := newVerifierRegistry()
	led.Adopt(reapReq(autonomous.VerifyHolder("gone")), "worker")
	led.Adopt(reapReq("task"), "")
	got := reapPass(led, reg, nil, time.Now().Add(11*time.Minute))
	if len(got) != 2 {
		t.Fatalf("reaped %v, want both", got)
	}
}

func TestLeaseReaperNeverExceedsPoolLimitWhileVerifiersRun(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	reg := newVerifierRegistry()
	holder := autonomous.VerifyHolder("t5")
	if err := led.Acquire(context.Background(), reapReq(holder), time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatal(err)
	}
	reg.begin(holder)
	now := time.Now().Add(20 * time.Minute)
	reapPass(led, reg, nil, now)
	// a second verifier must not get the slot while the first still runs
	err := led.Acquire(context.Background(), reapReq(autonomous.VerifyHolder("t6")), 50*time.Millisecond, func() bool { return false }, func(string) {})
	if err == nil {
		t.Fatal("second verifier admitted beyond pool limit")
	}
	for _, p := range led.Snapshot().Pools {
		if p.Name == "gpu" && p.Held > p.Limit {
			t.Fatalf("held %d > limit %d", p.Held, p.Limit)
		}
	}
}
