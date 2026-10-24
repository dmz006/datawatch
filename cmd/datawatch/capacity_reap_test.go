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

// Judge leases ("judge:<unit id>") are judged by the same live-session /
// spawn-grace logic as verifier leases, over their own registry instance
// (the holderAlive closure of the judges registry in wireCapacity).

func TestLeaseReaperKeepsLongRunningJudge(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	jreg := newVerifierRegistry() // the judges' registry, same type
	holder := autonomous.JudgeHolder("u1")
	led.Adopt(reapReq(holder), "")
	end := jreg.begin(holder)
	defer end()
	start := time.Now()
	// active judge (e.g. the /api/ask path) with no session: kept at every
	// age, exactly like the verifier rule.
	for _, d := range []time.Duration{time.Minute, 11 * time.Minute, 25 * time.Minute, 29 * time.Minute} {
		if got := reapPass(led, jreg, nil, start.Add(d)); len(got) != 0 {
			t.Fatalf("reaped %v at +%v", got, d)
		}
	}
	// bound judge session: its liveness decides.
	jreg.bindSession(holder, "j1")
	led.Bind(holder, "j1")
	if got := reapPass(led, jreg, map[string]bool{"j1": true}, start.Add(30*time.Minute)); len(got) != 0 {
		t.Fatalf("reaped %v with live judge session", got)
	}
	if !led.Held(holder) {
		t.Fatal("lease lost")
	}
}

func TestLeaseReaperReapsJudgeLeaseWhoseSessionCompleted(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	jreg := newVerifierRegistry()
	holder := autonomous.JudgeHolder("u2")
	led.Adopt(reapReq(holder), "")
	end := jreg.begin(holder)
	jreg.bindSession(holder, "j2")
	led.Bind(holder, "j2")
	// The bound session is dead: same rule as the verifier lease — the
	// session decides, the registration does not keep a dead lease alive.
	got := reapPass(led, jreg, map[string]bool{"j2": false}, time.Now())
	end()
	if len(got) != 1 || got[0] != holder {
		t.Fatalf("reaped %v, want [%s]", got, holder)
	}
}

func TestLeaseReaperReapsLeakedUnregisteredJudgeLease(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	jreg := newVerifierRegistry()
	lost := autonomous.JudgeHolder("gone")
	fresh := autonomous.JudgeHolder("fresh")
	led.Adopt(reapReq(lost), "")
	led.Adopt(reapReq(fresh), "")
	// within spawn grace: kept even though unregistered
	if got := reapPass(led, jreg, nil, time.Now()); len(got) != 0 {
		t.Fatalf("reaped %v inside spawn grace", got)
	}
	// after the grace expires: reaped exactly like the unregistered verifier
	// lease.
	got := reapPass(led, jreg, nil, time.Now().Add(11*time.Minute))
	if len(got) != 2 {
		t.Fatalf("reaped %v, want both judge leases", got)
	}
}
