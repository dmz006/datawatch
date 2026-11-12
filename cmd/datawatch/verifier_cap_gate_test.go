package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// TestHeadroomWiring_ProductionConstructorSharesOneStore proves the production
// constructor hands one store to the REST server and to the admission gate:
// the store attached to the server is the very instance the gate reads.
func TestHeadroomWiring_ProductionConstructorSharesOneStore(t *testing.T) {
	cfg := config.DefaultConfig()
	hw := newHeadroomWiring(cfg)
	if hw.Store == nil {
		t.Fatal("newHeadroomWiring: nil store")
	}
	if hw.Gate.Store != hw.Store {
		t.Fatal("admission gate reads a different store than the one the wiring built")
	}
	srv := server.New(&cfg.Server, cfg, "", t.TempDir(), nil, "test-host", nil)
	hw.attach(srv)
	if got := srv.HeadroomStore(); got != hw.Store {
		t.Fatalf("REST server store = %p, want the one shared store %p", got, hw.Store)
	}
	// A second construction is a second store: the sharing comes from the
	// constructor being called once, not from a package-level variable.
	if other := newHeadroomWiring(cfg); other.Store == hw.Store {
		t.Fatal("two constructions returned the same store; the store must be per start")
	}
	// Attaching to an absent server is a no-op, not a panic.
	hw.attach(nil)
}

// TestVerifierCapGate_OneAgeAndFloorDriveBothGates changes the autonomous
// freshness bound and the free-memory floor in one config and observes both
// gates change together: the admission gate (the pool cap the capacity sync
// applies) and the verifier_slots write gate (the REST node save). Both read
// the same store, through the same config accessors.
func TestVerifierCapGate_OneAgeAndFloorDriveBothGates(t *testing.T) {
	cfg := config.DefaultConfig()
	hw := newHeadroomWiring(cfg)
	srv := server.New(&cfg.Server, cfg, "", t.TempDir(), nil, "test-host", nil)
	hw.attach(srv)

	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	srv.SetComputeRegistry(reg)

	// One measurement taken 30 hours ago: 4096 MB free, 1024 MB per request.
	// Cap 3 needs 2048 MB on top of the floor: 4096 - 1024 = 3072 covers it
	// when fresh; a 24-hour bound makes it stale.
	hw.Store.Put(compute.HeadroomMeasurement{Node: "gpubox", FreeMB: 4096, PerRequestMB: 1024, MeasuredAt: time.Now().Add(-30 * time.Hour)})

	nodes := []verifierNodeInfo{{node: &compute.Node{Name: "gpubox", VerifierSlots: ptrInt(3)}, hosting: true, executor: 3}}
	admit := func() (int, string) {
		led := capacity.New(capacity.Options{})
		cache := newVerifierNodeCache()
		syncVerifierPools(led, cache, hw.Gate, noObserverRead(), nodes)
		st, _ := cache.VerifierNodeStateOf("gpubox")
		return led.Limit(capacity.VerifierPool("gpubox")), st.CapRefusal
	}
	// write saves the node with the given verifier_slots. A save that raises
	// the stored value is gated; one that leaves it unchanged is not.
	write := func(slots int) int {
		body := fmt.Sprintf(`{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_slots":%d}`, slots)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
		return rec.Code
	}

	// Step 1: the default 24-hour bound makes the measurement stale. Both gates refuse.
	if got, why := admit(); got != 1 || !strings.Contains(why, "measurement stale") {
		t.Fatalf("admission (24h) = cap %d reason %q, want cap 1 and a stale refusal", got, why)
	}
	if code := write(3); code != http.StatusConflict {
		t.Fatalf("write (24h) status = %d, want 409", code)
	}

	// Step 2: raise the freshness bound to 48 hours. Both gates accept cap 3.
	cfg.Autonomous.VerifierHeadroomMaxAgeHours = 48
	if got, why := admit(); got != 3 || why != "" {
		t.Fatalf("admission (48h) = cap %d reason %q, want cap 3 and no refusal", got, why)
	}
	if code := write(3); code != http.StatusOK {
		t.Fatalf("write (48h) status = %d, want 200", code)
	}

	// Step 3: raise the floor to 3584 MB. 4096 - 3584 = 512 < 2048 needed for
	// cap 3, so admission refuses cap 3 again. The stored value is already 3,
	// so re-saving it is not a raise and is not gated; raising to 4 is gated
	// with the floor shortfall for cap 4: 4096 - 3584 = 512 < 3072 needed.
	floor := 3584
	cfg.Autonomous.VerifierMinFreeMemMB = &floor
	got, why := admit()
	if got != 1 || !strings.Contains(why, "shortfall 1536 MB") {
		t.Fatalf("admission (floor 3584) = cap %d reason %q, want cap 1 with shortfall 1536 MB", got, why)
	}
	if code := write(4); code != http.StatusConflict {
		t.Fatalf("raise to 4 (floor 3584) status = %d, want 409", code)
	}
}
