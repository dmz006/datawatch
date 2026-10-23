// Wiring test for the peer usage cache: the gate carries a cache, the
// syncer's AfterPass refreshes that same cache, and the gate blocks on a
// confirmed match only once the cache has the peer's usage.

package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestFingerprintGateUsesRefreshedPeerUsageCache(t *testing.T) {
	resetFingerprintGate(t)

	local := localNodeExporter("gpu1", testGPU)
	peer := localNodeExporter("peerNode", testGPU)
	srv, _ := peerFingerprintServer(t, peer, "tok", http.StatusOK, "node:peerNode", 1)
	st := newTestPeerStore(t, srv.URL, "tok", true)

	store := testFingerprintStore(t)
	syncer := newFingerprintSyncer(st, local, store)
	syncer.SyncOnce(context.Background())
	confirmSharedMatch(t, store, "gpu1")

	localHeld := func(pool string) (int, int) {
		if pool == "node:gpu1" {
			return 1, 2
		}
		return 0, 0
	}
	gate := newFingerprintGate(st, store, localHeld)
	if gate.Usage == nil {
		t.Fatal("gate.Usage is nil, want the peer usage cache")
	}
	syncer.AfterPass = gate.Usage.RefreshAll

	if why := gate.Reason("gpu1"); why != "" {
		t.Fatalf("gate blocked before the cache was refreshed: %q", why)
	}

	// The cold Reason above may have started a background fetch that holds
	// the in-flight claim, so retry the refresh until the cache is warm.
	deadline := time.Now().Add(3 * time.Second)
	for {
		syncer.AfterPass(context.Background())
		if gate.Reason("gpu1") != "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("gate admitted after refresh, want blocked by the peer's cached usage")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
