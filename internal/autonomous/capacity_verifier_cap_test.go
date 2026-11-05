package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// fixedNow is the fixed clock every verifier-cap test runs on: no test
// depends on wall time.
var fixedNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func verCapPolicy(slots int, store *compute.HeadroomStore, now time.Time) *VerifierCapPolicy {
	return &VerifierCapPolicy{Node: "n1", Slots: slots, Headroom: store, Now: func() time.Time { return now }}
}

// acquireNode admits holder on node:n1 and must succeed.
func acquireNode(t *testing.T, led *capacity.Ledger, holder string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := capacity.Request{Holder: holder, PRDID: "prd", Pools: []string{"node:n1"}, Node: "n1"}
	if err := led.Acquire(ctx, req, 0, nil, nil); err != nil {
		t.Fatalf("acquire %s: %v", holder, err)
	}
	t.Cleanup(func() { led.Release(holder) })
}

// refuseNode expects holder to be refused on node:n1 with a wait reason
// containing wantSub.
func refuseNode(t *testing.T, led *capacity.Ledger, holder, wantSub string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := capacity.Request{Holder: holder, PRDID: "prd", Pools: []string{"node:n1"}, Node: "n1"}
	err := led.Acquire(ctx, req, 150*time.Millisecond, nil, nil)
	if err == nil {
		led.Release(holder)
		t.Fatalf("holder %s: expected refusal, got admission", holder)
	}
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("holder %s: want %v, got %v", holder, capacity.ErrWaitTimeout, err)
	}
	if !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("holder %s: want reason containing %q, got %q", holder, wantSub, err)
	}
}

func newVerCapLedger(t *testing.T, nodeLimit int, vc capacity.VerifierCap) *capacity.Ledger {
	t.Helper()
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:n1", nodeLimit)
	led.SetVerifierNodeCap("n1", vc)
	return led
}

// Configured 2, no measurement: the gate refuses and the node runs at 1.
// Only verify: holders count against the cap; non-verifier admission on the
// node is unaffected by the cap.
func TestVerifierCap_NoMeasurementRunsAtOne(t *testing.T) {
	p := verCapPolicy(2, compute.NewHeadroomStore(), fixedNow)
	eff, refusal := p.Effective()
	if eff != 1 || refusal == nil {
		t.Fatalf("no measurement: want effective 1 with refusal, got %d %v", eff, refusal)
	}
	if refusal.Node != "n1" || !strings.Contains(refusal.Error(), "no fresh measurement") {
		t.Fatalf("refusal should name node n1 and the missing measurement: %v", refusal)
	}
	vc := p.SnapshotCap()
	if vc.Configured != 2 || vc.Effective != 1 || vc.Refusal == "" {
		t.Fatalf("snapshot should carry configured 2, effective 1 and the refusal: %+v", vc)
	}

	led := newVerCapLedger(t, 4, vc)
	got, ok := led.VerifierNodeCap("n1")
	if !ok || got.Configured != 2 || got.Effective != 1 {
		t.Fatalf("ledger should record the cap, got %+v ok=%v", got, ok)
	}

	acquireNode(t, led, "verify:t1")
	refuseNode(t, led, "verify:t2", "verifier cap")
	// A non-verifier holder is not blocked by the cap: the node limit (4)
	// still has room.
	acquireNode(t, led, "task:t3")
	// Releasing the verifier frees the cap slot: the waiting one now admits.
	led.Release("verify:t1")
	acquireNode(t, led, "verify:t2")
}

// Configured 2 with a fresh covering measurement: the node runs at 2.
func TestVerifierCap_FreshMeasurementRunsAtConfigured(t *testing.T) {
	store := compute.NewHeadroomStore()
	store.Put(compute.HeadroomMeasurement{Node: "n1", FreeMB: 16384, PerRequestMB: 2048, MeasuredAt: fixedNow.Add(-time.Minute)})
	p := verCapPolicy(2, store, fixedNow)
	eff, refusal := p.Effective()
	if eff != 2 || refusal != nil {
		t.Fatalf("fresh covering measurement: want effective 2, got %d %v", eff, refusal)
	}
	vc := p.SnapshotCap()
	if vc.Configured != 2 || vc.Effective != 2 || vc.Refusal != "" {
		t.Fatalf("snapshot should carry configured 2, effective 2, no refusal: %+v", vc)
	}

	led := newVerCapLedger(t, 4, vc)
	acquireNode(t, led, "verify:t1")
	acquireNode(t, led, "verify:t2")
	refuseNode(t, led, "verify:t3", "verifier cap")
	// The cap is separate from the node limit: a worker still admits (3/4).
	acquireNode(t, led, "task:t4")
}

// Worker leases never count against the verifier cap: two workers on the
// node change nothing for the verifiers, and the snapshot counts only the
// verify: leases as held.
func TestVerifierCap_WorkersDoNotCountAgainstCap(t *testing.T) {
	store := compute.NewHeadroomStore()
	store.Put(compute.HeadroomMeasurement{Node: "n1", FreeMB: 16384, PerRequestMB: 2048, MeasuredAt: fixedNow.Add(-time.Minute)})
	// Node limit 8 leaves pool headroom, so the verifier cap is the binding
	// constraint, not the node limit.
	led := newVerCapLedger(t, 8, verCapPolicy(2, store, fixedNow).SnapshotCap())

	acquireNode(t, led, "task:t1")
	acquireNode(t, led, "task:t2")
	acquireNode(t, led, "verify:v1")
	acquireNode(t, led, "verify:v2") // the workers did not consume the cap
	refuseNode(t, led, "verify:v3", "verifier cap")

	var ps *capacity.PoolStatus
	st := led.Snapshot()
	for i := range st.Pools {
		if st.Pools[i].Name == "node:n1" {
			ps = &st.Pools[i]
		}
	}
	if ps == nil || ps.VerifierCap == nil {
		t.Fatalf("node:n1 pool should carry the verifier cap in the snapshot")
	}
	if ps.VerifierCap.Held != 2 {
		t.Fatalf("snapshot should count the two verify: leases, not the workers: held=%d", ps.VerifierCap.Held)
	}
	if ps.Held != 4 {
		t.Fatalf("pool should still count every lease: held=%d", ps.Held)
	}
}

// A measurement older than MaxAge is a stale measurement: the node runs at 1.
func TestVerifierCap_StaleMeasurementRunsAtOne(t *testing.T) {
	store := compute.NewHeadroomStore()
	store.Put(compute.HeadroomMeasurement{Node: "n1", FreeMB: 16384, PerRequestMB: 2048, MeasuredAt: fixedNow.Add(-2 * time.Hour)})
	p := verCapPolicy(2, store, fixedNow)
	eff, refusal := p.Effective()
	if eff != 1 || refusal == nil || !strings.Contains(refusal.Error(), "stale") {
		t.Fatalf("stale measurement: want effective 1 with a stale refusal, got %d %v", eff, refusal)
	}
	led := newVerCapLedger(t, 4, p.SnapshotCap())
	acquireNode(t, led, "verify:t1")
	refuseNode(t, led, "verify:t2", "verifier cap")
}

// verifier_slots absent or 0 means the default cap of 1, which is always
// supported and therefore never refused.
func TestVerifierCap_ZeroSlotsIsDefault(t *testing.T) {
	p := verCapPolicy(0, nil, fixedNow)
	eff, refusal := p.Effective()
	if eff != 1 || refusal != nil {
		t.Fatalf("zero slots: want default cap 1, no refusal, got %d %v", eff, refusal)
	}
	if vc := p.SnapshotCap(); vc.Configured != 1 || vc.Effective != 1 || vc.Refusal != "" {
		t.Fatalf("snapshot: %+v", vc)
	}
}

// The verifier's own admission path (the manager's JudgeFits under
// VerifyHolder) is blocked by the cap while a worker's fits.
func TestVerifierCap_ManagerFitsPath(t *testing.T) {
	m, _, led := capFixture(t, 4)
	led.SetVerifierNodeCap("n1", capacity.VerifierCap{Configured: 2, Effective: 1})

	acquireNode(t, led, "verify:t1")
	if m.JudgeFits(JudgeAdmit{Holder: "verify:t2"}) {
		t.Fatal("second verifier should not fit while the cap is full")
	}
	if !m.JudgeFits(JudgeAdmit{Holder: "task:t3"}) {
		t.Fatal("a non-verifier holder fits: the cap must not block it")
	}
	led.Release("verify:t1")
	if !m.JudgeFits(JudgeAdmit{Holder: "verify:t2"}) {
		t.Fatal("after the verifier releases, the next one fits")
	}
}
