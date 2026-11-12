package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// fakeHeadroomProbe is a HeadroomProbe that records what Measure asked it.
type fakeHeadroomProbe struct {
	freeMB     int
	resident   string
	perRequest int
	readErr    error

	reads    int
	requests []string // models the probe request was sent for
}

func (p *fakeHeadroomProbe) ReadFreeMB(context.Context, compute.Node) (int, string, error) {
	p.reads++
	if p.readErr != nil {
		return 0, "", p.readErr
	}
	return p.freeMB, p.resident, nil
}

func (p *fakeHeadroomProbe) RequestMemoryMB(_ context.Context, _ compute.Node, model string) (int, error) {
	p.requests = append(p.requests, model)
	return p.perRequest, nil
}

// measurerFixture is one node "gpubox" with a configured cap of 3, the probe,
// a settable clock and the measurer over the given store.
type measurerFixture struct {
	probe *fakeHeadroomProbe
	now   time.Time
	held  int
	m     *verifierHeadroomMeasurer
}

func newMeasurerFixture(t *testing.T, store *compute.HeadroomStore, cfgSlots int) *measurerFixture {
	t.Helper()
	f := &measurerFixture{
		probe: &fakeHeadroomProbe{freeMB: 8192, resident: "qwen:4b", perRequest: 1024},
		now:   time.Now(), // the admission gate reads the real clock, so the fixture starts at it
	}
	node := &compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", VerifierSlots: ptrInt(cfgSlots)}
	f.m = newVerifierHeadroomMeasurer(store,
		func() []*compute.Node { return []*compute.Node{node} },
		func(n *compute.Node) int { return compute.EffectiveVerifierSlots(n.VerifierSlots, true) },
		func(string) int { return f.held },
		func(context.Context, *compute.Node) ([]string, bool) {
			return []string{"qwen:30b", "qwen:4b"}, true
		},
		f.probe,
		func() time.Time { return f.now },
	)
	return f
}

// TestVerifierHeadroomMeasurer_FillsSharedStoreForIdleNode is the production
// writer test: a tick on an idle node above cap 1 puts a measurement into the
// store, probing with the smallest served model.
func TestVerifierHeadroomMeasurer_FillsSharedStoreForIdleNode(t *testing.T) {
	hw := newHeadroomWiring(config.DefaultConfig())
	f := newMeasurerFixture(t, hw.Store, 3)

	measured := f.m.tick(context.Background())
	if len(measured) != 1 || measured[0] != "gpubox" {
		t.Fatalf("tick measured %v, want [gpubox]", measured)
	}
	got, ok := hw.Store.Get("gpubox")
	if !ok {
		t.Fatal("tick did not put a measurement into the shared store")
	}
	if got.FreeMB != 8192 || got.PerRequestMB != 1024 || got.ResidentModel != "qwen:4b" {
		t.Errorf("stored measurement = %+v, want free 8192, per request 1024, resident qwen:4b", got)
	}
	if len(f.probe.requests) != 1 || f.probe.requests[0] != "qwen:4b" {
		t.Errorf("probe request model = %v, want the smallest served model qwen:4b", f.probe.requests)
	}
}

// TestVerifierHeadroomMeasurer_SkipsBusyAndSingleSlotNodes: a node with a
// lease is never measured (the probe is not even asked), and a node whose
// cap is 1 needs no measurement at all.
func TestVerifierHeadroomMeasurer_SkipsBusyAndSingleSlotNodes(t *testing.T) {
	store := compute.NewHeadroomStore()

	busy := newMeasurerFixture(t, store, 3)
	busy.held = 1
	if got := busy.m.tick(context.Background()); len(got) != 0 {
		t.Fatalf("busy node measured: %v", got)
	}
	if busy.probe.reads != 0 {
		t.Errorf("busy node probed %d times, want 0", busy.probe.reads)
	}
	if _, ok := store.Get("gpubox"); ok {
		t.Error("busy node put a measurement in the store")
	}

	single := newMeasurerFixture(t, store, 1)
	if got := single.m.tick(context.Background()); len(got) != 0 {
		t.Fatalf("cap-1 node measured: %v", got)
	}
	if single.probe.reads != 0 {
		t.Errorf("cap-1 node probed %d times, want 0", single.probe.reads)
	}
}

// TestVerifierHeadroomMeasurer_RemeasuresOnlyAfterInterval: a fresh stored
// measurement is left alone; one older than the re-measure interval is taken
// again.
func TestVerifierHeadroomMeasurer_RemeasuresOnlyAfterInterval(t *testing.T) {
	store := compute.NewHeadroomStore()
	f := newMeasurerFixture(t, store, 3)
	ctx := context.Background()

	f.m.tick(ctx)
	f.now = f.now.Add(verifierMeasureEvery - time.Minute)
	if got := f.m.tick(ctx); len(got) != 0 || f.probe.reads != 1 {
		t.Fatalf("fresh measurement re-taken: measured %v, reads %d", got, f.probe.reads)
	}
	f.now = f.now.Add(2 * time.Minute)
	if got := f.m.tick(ctx); len(got) != 1 || f.probe.reads != 2 {
		t.Fatalf("stale measurement not re-taken: measured %v, reads %d", got, f.probe.reads)
	}
}

// TestVerifierHeadroomMeasurer_FailureStoresNothing: a probe failure leaves
// the store empty, so the admission gate keeps the cap at 1 with the
// no-measurement refusal.
func TestVerifierHeadroomMeasurer_FailureStoresNothing(t *testing.T) {
	hw := newHeadroomWiring(config.DefaultConfig())
	f := newMeasurerFixture(t, hw.Store, 3)
	f.probe.readErr = errors.New("observer unreachable")

	if got := f.m.tick(context.Background()); len(got) != 0 {
		t.Fatalf("failed probe reported as measured: %v", got)
	}
	if _, ok := hw.Store.Get("gpubox"); ok {
		t.Fatal("failed probe stored a measurement")
	}
}

// TestVerifierHeadroomMeasurer_FeedsAdmissionGateThroughOneStore: the
// measurement the loop writes is what the admission gate reads. Before the
// tick the cap is held at 1 with a "no fresh measurement" refusal; after it
// the cap of 3 is applied. Both sides hold the same store instance.
func TestVerifierHeadroomMeasurer_FeedsAdmissionGateThroughOneStore(t *testing.T) {
	cfg := config.DefaultConfig()
	hw := newHeadroomWiring(cfg)
	f := newMeasurerFixture(t, hw.Store, 3)
	if f.m.store != hw.Gate.Store {
		t.Fatal("measurer and admission gate read different stores")
	}
	nodes := []verifierNodeInfo{{node: &compute.Node{Name: "gpubox", VerifierSlots: ptrInt(3)}, hosting: true, executor: 3}}
	admit := func() (int, string) {
		led := capacity.New(capacity.Options{})
		cache := newVerifierNodeCache()
		syncVerifierPools(led, cache, hw.Gate, noObserverRead(), nodes)
		st, _ := cache.VerifierNodeStateOf("gpubox")
		return led.Limit(capacity.VerifierPool("gpubox")), st.CapRefusal
	}

	if got, why := admit(); got != 1 || !strings.Contains(why, "no fresh measurement") {
		t.Fatalf("before tick: cap %d reason %q, want 1 with no-measurement refusal", got, why)
	}
	f.m.tick(context.Background())
	if got, why := admit(); got != 3 || why != "" {
		t.Fatalf("after tick: cap %d reason %q, want 3 and no refusal", got, why)
	}
}

// TestVerifierHeadroomMeasurer_HeldLeaseStopsRemeasure: a node that becomes
// busy after a measurement is not measured again, and its old measurement
// ages out under the configured freshness bound.
func TestVerifierHeadroomMeasurer_HeldLeaseStopsRemeasure(t *testing.T) {
	store := compute.NewHeadroomStore()
	f := newMeasurerFixture(t, store, 3)
	ctx := context.Background()
	f.m.tick(ctx)

	f.now = f.now.Add(time.Hour)
	f.held = 2
	f.now = f.now.Add(verifierMeasureEvery)
	if got := f.m.tick(ctx); len(got) != 0 || f.probe.reads != 1 {
		t.Fatalf("busy node re-measured: measured %v, reads %d", got, f.probe.reads)
	}
	if _, ok := store.Get("gpubox"); !ok {
		t.Fatal("busy node lost its earlier measurement; it should age out, not be deleted")
	}
}

// TestRecordConfigVerifierSlotsRefusals: at config load a configured
// verifier_slots above 1 with no supporting measurement is recorded as a
// refusal and the value is kept in the config; once a fresh measurement
// supports it, nothing is recorded.
func TestRecordConfigVerifierSlotsRefusals(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ComputeNodes = []config.ComputeNodeYAML{
		{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", VerifierSlots: ptrInt(3)},
		{Name: "small", Kind: "ollama", Address: "http://small:11434", VerifierSlots: ptrInt(1)},
	}
	store := compute.NewHeadroomStore()
	now := time.Now()

	refused := recordConfigVerifierSlotsRefusals(store, cfg, now)
	if len(refused) != 1 || !strings.Contains(refused[0], "cap on node gpubox refused: no fresh measurement") {
		t.Fatalf("refusals = %v, want one naming gpubox with no fresh measurement", refused)
	}
	if cfg.ComputeNodes[0].VerifierSlots == nil || *cfg.ComputeNodes[0].VerifierSlots != 3 {
		t.Fatal("refusal changed the configured verifier_slots; the value must be kept")
	}

	store.Put(compute.HeadroomMeasurement{Node: "gpubox", FreeMB: 8192, PerRequestMB: 1024, MeasuredAt: now})
	if refused := recordConfigVerifierSlotsRefusals(store, cfg, now); len(refused) != 0 {
		t.Fatalf("supported cap still refused: %v", refused)
	}
	if got := recordConfigVerifierSlotsRefusals(store, nil, now); got != nil {
		t.Fatalf("nil config returned %v", got)
	}
}
