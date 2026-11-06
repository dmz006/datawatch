// Verifier headroom measurer — the production writer of the daemon's one
// headroom store.
//
// The admission gate and the verifier_slots write gate only read the store
// (compute.HeadroomStore); without this loop nothing would ever fill it, and
// every verifier cap above 1 would stay clamped to 1. The measurer runs on
// its own goroutine, never under the ledger lock, and does the probing I/O
// outside it:
//
//   - only nodes whose configured verifier cap is above 1 are measured;
//     a node at 1 or below needs no measurement to keep its cap;
//   - a node that holds any lease (executor, verifier, external or overflow)
//     is never measured: compute.Measure refuses it with ErrNodeBusy, and the
//     loop does not even ask;
//   - a node measured within the re-measure interval is left alone, so the
//     stored measurement stays fresh while the node is idle. Once the node
//     stops being idle the measurement ages out under
//     autonomous.verifier_headroom_max_age_hours and the gates refuse again.
//
// Measurement goes through compute.Measure with the Ollama probe
// (compute.OllamaHeadroomProbe), the same path the probe tests exercise. The
// allow-list is the node's served models, smallest first, read through the
// served-models cache.
package main

import (
	"context"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/server"
)

const (
	// verifierMeasureTick is how often the loop looks at the nodes.
	verifierMeasureTick = 30 * time.Second
	// verifierMeasureEvery is how old a stored measurement may be before an
	// idle node is measured again. It is well inside the 24-hour default
	// freshness bound, so an idle node's measurement never goes stale.
	verifierMeasureEvery = 5 * time.Minute
	// verifierMeasureTimeout bounds one node's measurement.
	verifierMeasureTimeout = 60 * time.Second
)

// verifierHeadroomMeasurer holds the injected inputs of the measurement loop.
// Every input is a function or interface, so the tests drive the loop with a
// fake probe, a fake clock and fake nodes.
type verifierHeadroomMeasurer struct {
	store *compute.HeadroomStore
	// nodes lists the enabled compute nodes at each tick.
	nodes func() []*compute.Node
	// configured is the verifier slot count the node takes
	// (compute.EffectiveVerifierSlots with the node's hosting state).
	configured func(n *compute.Node) int
	// held is the number of leases on the node now (executor, verifier,
	// external and overflow). Read from the ledger, never under its lock.
	held func(node string) int
	// models returns the node's served models; ok is false when they could
	// not be read.
	models func(ctx context.Context, n *compute.Node) (names []string, ok bool)
	probe  compute.HeadroomProbe
	now    func() time.Time
	every  time.Duration

	// lastErr is the last error logged per node, so a persistent failure is
	// logged once per change rather than every tick.
	lastErr map[string]string
}

// newVerifierHeadroomMeasurer builds the loop over store. every defaults to
// verifierMeasureEvery when zero.
func newVerifierHeadroomMeasurer(store *compute.HeadroomStore, nodes func() []*compute.Node,
	configured func(n *compute.Node) int, held func(node string) int,
	models func(ctx context.Context, n *compute.Node) ([]string, bool),
	probe compute.HeadroomProbe, now func() time.Time) *verifierHeadroomMeasurer {
	return &verifierHeadroomMeasurer{
		store: store, nodes: nodes, configured: configured, held: held,
		models: models, probe: probe, now: now, every: verifierMeasureEvery,
		lastErr: map[string]string{},
	}
}

// tick measures every idle node whose configured cap is above 1 and whose
// stored measurement is missing or older than every. It returns the nodes it
// measured, for the tests.
func (m *verifierHeadroomMeasurer) tick(ctx context.Context) []string {
	var measured []string
	for _, n := range m.nodes() {
		if n == nil || n.Name == "" || m.configured(n) <= 1 {
			continue
		}
		if prev, ok := m.store.Get(n.Name); ok && m.now().Sub(prev.MeasuredAt) < m.every {
			continue
		}
		if m.held(n.Name) > 0 {
			continue
		}
		if m.measure(ctx, n) {
			measured = append(measured, n.Name)
		}
	}
	return measured
}

// measure takes one measurement of n and stores it. It reports whether a
// measurement was stored; a failure is logged once per distinct message.
func (m *verifierHeadroomMeasurer) measure(ctx context.Context, n *compute.Node) bool {
	mctx, cancel := context.WithTimeout(ctx, verifierMeasureTimeout)
	defer cancel()
	names, ok := m.models(mctx, n)
	if !ok || len(names) == 0 {
		m.logOnce(n.Name, "no served models could be read")
		return false
	}
	meas, err := compute.Measure(mctx, compute.MeasureDeps{
		Probe:         m.probe,
		Now:           m.now,
		Held:          m.held(n.Name),
		AllowedModels: compute.SortSmallestFirst(names),
		Node:          *n,
	})
	if err != nil {
		m.logOnce(n.Name, err.Error())
		return false
	}
	m.store.Put(meas)
	delete(m.lastErr, n.Name)
	return true
}

func (m *verifierHeadroomMeasurer) logOnce(node, msg string) {
	if m.lastErr[node] == msg {
		return
	}
	m.lastErr[node] = msg
	log.Printf("[capacity] verifier headroom measurement for node %s not stored: %s", node, msg)
}

// run ticks until ctx is done. It is started once per daemon, after the
// served-models cache is wired.
func (m *verifierHeadroomMeasurer) run(ctx context.Context) {
	t := time.NewTicker(verifierMeasureTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

// startVerifierHeadroomMeasurer is the production start: it builds the loop
// over the daemon's one store, the live compute registry, the ledger's lease
// counts, the served-models cache and the Ollama probe reading the observer,
// then runs it in its own goroutine.
func startVerifierHeadroomMeasurer(ctx context.Context, store *compute.HeadroomStore, httpServer *server.HTTPServer,
	led *capacity.Ledger, cache *compute.ServedModelsCache) *verifierHeadroomMeasurer {
	nodes := func() []*compute.Node {
		reg := httpServer.ComputeRegistry()
		if reg == nil {
			return nil
		}
		var enabled []*compute.Node
		for _, n := range reg.List() {
			if !n.Disabled {
				enabled = append(enabled, n)
			}
		}
		return enabled
	}
	configured := func(n *compute.Node) int {
		hosting := false
		for _, entryNodes := range engineNodes(httpServer.InferenceRegistry()) {
			for _, node := range entryNodes {
				if node == n.Name {
					hosting = true
				}
			}
		}
		return compute.EffectiveVerifierSlots(n.VerifierSlots, hosting)
	}
	held := func(node string) int {
		total := 0
		for _, p := range led.Snapshot().Pools {
			if p.Name == "node:"+node || p.Name == capacity.VerifierPool(node) {
				total += p.Held + p.External + p.Overflow
			}
		}
		return total
	}
	models := func(ctx context.Context, n *compute.Node) ([]string, bool) {
		if cache == nil {
			return nil, false
		}
		sm := cache.Get(ctx, n, false)
		if !sm.Known {
			return nil, false
		}
		return sm.Models, true
	}
	observe := func(n compute.Node) compute.NodeObservation {
		o, _ := httpServer.NodeObservation(n.Name)
		return o
	}
	m := newVerifierHeadroomMeasurer(store, nodes, configured, held, models,
		compute.NewOllamaHeadroomProbe(observe, nil), time.Now)
	go m.run(ctx)
	return m
}
