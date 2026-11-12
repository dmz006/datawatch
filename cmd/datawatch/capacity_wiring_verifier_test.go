package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// ptrInt is the test's *int helper for a node's verifier_slots value.
func ptrInt(v int) *int { return &v }

// noObserverRead is the fake observer that has no data for any node.
func noObserverRead() verifierObserverRead {
	return func(name string) (compute.NodeObservation, bool) {
		return compute.NodeObservation{}, false
	}
}

// TestSyncVerifierPools_CapAppliedToNodeVerifyPool pins wiring behavior: the
// verifier half of syncPools applies compute.EffectiveVerifierSlots to the
// node's node-verify: pool, alongside (not instead of) the executor node:
// pool it already manages, and records both caps in the status cache.
func TestSyncVerifierPools_CapAppliedToNodeVerifyPool(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	// A fresh measurement that supports cap 3 (2 extra requests of 512 MB on
	// 8 GB free, well above the 1024 MB floor), so the configured cap applies.
	store := compute.NewHeadroomStore()
	store.Put(compute.HeadroomMeasurement{Node: "gpu-a", FreeMB: 8192, PerRequestMB: 512, MeasuredAt: time.Now()})
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "gpu-a", VerifierSlots: ptrInt(3)}, hosting: true, executor: 4},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(store, &config.Config{}), noObserverRead(), nodes)

	if got := led.Limit(capacity.VerifierPool("gpu-a")); got != 3 {
		t.Fatalf("Limit(node-verify:gpu-a) = %d, want 3 (explicit verifier_slots)", got)
	}
	// The daemon's exposed status function reads the same cache wireCapacity
	// publishes; point it at this test's cache for the duration of the check.
	old := verifierHeadroomCache
	verifierHeadroomCache = cache
	defer func() { verifierHeadroomCache = old }()
	st, ok := VerifierNodeStatus("gpu-a")
	if !ok {
		t.Fatal("VerifierNodeStatus(gpu-a): ok=false, want the node's record")
	}
	if st.ExecutorCap != 4 || st.VerifierCap != 3 {
		t.Errorf("status caps = executor %d, verifier %d; want executor 4, verifier 3", st.ExecutorCap, st.VerifierCap)
	}
}

// TestSyncVerifierPools_ExplicitZeroLeavesNoVerifierPool pins the inverted
// rule for this pool: an explicit verifier_slots of 0 sets a limit of 0,
// which means the node takes NO verifier pool (not unlimited).
func TestSyncVerifierPools_ExplicitZeroLeavesNoVerifierPool(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "gpu-b", VerifierSlots: ptrInt(0)}, hosting: true, executor: 2},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(nil, &config.Config{}), noObserverRead(), nodes)

	if got := led.Limit(capacity.VerifierPool("gpu-b")); got != 0 {
		t.Fatalf("Limit(node-verify:gpu-b) = %d, want 0 (explicit 0 = no verifier pool)", got)
	}
	st, ok := cache.VerifierNodeStateOf("gpu-b")
	if !ok || st.VerifierCap != 0 {
		t.Fatalf("status = %+v ok=%v, want verifier cap 0", st, ok)
	}
}

// TestSyncVerifierPools_HostingDefaultSlots pins the unset default: a node
// that hosts a local engine takes 1 verifier slot; one that does not takes
// none (pool limit 0).
func TestSyncVerifierPools_HostingDefaultSlots(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "host"}, hosting: true},
		{node: &compute.Node{Name: "plain"}, hosting: false},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(nil, &config.Config{}), noObserverRead(), nodes)

	if got := led.Limit(capacity.VerifierPool("host")); got != 1 {
		t.Errorf("Limit(node-verify:host) = %d, want 1 (hosting default)", got)
	}
	if got := led.Limit(capacity.VerifierPool("plain")); got != 0 {
		t.Errorf("Limit(node-verify:plain) = %d, want 0 (non-hosting default)", got)
	}
	// No context size configured: the advice figure is absent, not zero.
	if st, _ := cache.VerifierNodeStateOf("host"); st.SuggestedCap != nil {
		t.Errorf("status SuggestedCap = %d, want none (no context size)", *st.SuggestedCap)
	}
}

// gpuObsWithFree builds one node observation with one GPU holding freeMB of
// free memory on a 2 GiB card.
func gpuObsWithFree(freeMB int) compute.NodeObservation {
	total := uint64(2 * 1024 * 1024 * 1024)
	free := uint64(freeMB) * 1024 * 1024
	return compute.NodeObservation{GPUs: []compute.GPUMemory{{TotalBytes: total, UsedBytes: total - free}}}
}

// TestVerifierHeadroom_BelowMinFreeBlocks pins the gate: a cached free
// memory below the minimum a verifier slot wants (default 1024 MB) blocks
// new verifier work on the node, with the headroom shortfall as the reason.
func TestVerifierHeadroom_BelowMinFreeBlocks(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	read := func(name string) (compute.NodeObservation, bool) {
		return gpuObsWithFree(512), true
	}
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "gpu-c", VerifierSlots: ptrInt(1), VerifierContextMB: 256}, hosting: true, executor: 2},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(nil, &config.Config{}), read, nodes)

	blocked, reason := cache.blocked("gpu-c")
	if !blocked {
		t.Fatalf("blocked = false, want true (512 MB free below the 1024 MB minimum)")
	}
	if want := "512 MB free is below the minimum of 1024 MB"; reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
	st, ok := cache.VerifierNodeStateOf("gpu-c")
	if !ok || !st.HasData || st.HeadroomOK || st.FreeMB != 512 {
		t.Fatalf("status = %+v ok=%v, want data, headroom not ok, 512 MB free", st, ok)
	}
	// Advice figure: 512 MB free over a 256 MB context holds 2 slots.
	if st.SuggestedCap == nil || *st.SuggestedCap != 2 {
		t.Errorf("SuggestedCap = %v, want 2 (512 MB / 256 MB)", st.SuggestedCap)
	}
}

// TestVerifierHeadroom_NoObserverDataFailsOpen pins the open end: a node
// whose observer read failed or returned nothing is not blocked — the
// reason says there is no observer data — and an unknown node fails open
// the same way.
func TestVerifierHeadroom_NoObserverDataFailsOpen(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "gpu-d"}, hosting: true},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(nil, &config.Config{}), noObserverRead(), nodes)

	for _, name := range []string{"gpu-d", "never-seen"} {
		blocked, reason := cache.blocked(name)
		if blocked {
			t.Fatalf("%s: blocked = true, want false (no observer data fails open)", name)
		}
		if want := "no observer data"; reason != want {
			t.Errorf("%s: reason = %q, want %q", name, reason, want)
		}
	}
	if st, ok := cache.VerifierNodeStateOf("gpu-d"); !ok || st.HasData || !st.HeadroomOK {
		t.Errorf("status = %+v ok=%v, want no data and headroom open", st, ok)
	}
}

// TestVerifierHeadroom_ClosureDoesNotCallObserver pins the lock rule: the
// ledger invokes the headroom gate under its own lock, so the gate must be a
// pure cache read. The sync step may consult the observer once per node;
// every gate call after that must not.
func TestVerifierHeadroom_ClosureDoesNotCallObserver(t *testing.T) {
	led := capacity.New(capacity.Options{})
	cache := newVerifierNodeCache()
	calls := 0
	read := func(name string) (compute.NodeObservation, bool) {
		calls++
		return compute.NodeObservation{}, false
	}
	nodes := []verifierNodeInfo{
		{node: &compute.Node{Name: "gpu-e"}, hosting: true},
	}
	syncVerifierPools(led, cache, newVerifierCapGate(nil, &config.Config{}), read, nodes)
	if calls != 1 {
		t.Fatalf("observer reads during sync = %d, want 1 (one sample per node)", calls)
	}
	for i := 0; i < 3; i++ {
		if blocked, _ := cache.blocked("gpu-e"); blocked {
			t.Fatal("blocked = true, want false (no observer data)")
		}
	}
	if calls != 1 {
		t.Fatalf("headroom gate called the observer: reads = %d, want 1", calls)
	}
}
