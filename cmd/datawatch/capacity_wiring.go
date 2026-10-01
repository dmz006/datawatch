package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// capacityTextFn renders the ledger for the comm-channel `capacity` command.
var capacityTextFn func() string

// verifierCapacityAdmit lets the verifier's own /api/ask call (main.go's
// autonomousVerify closure, defined before amgr exists and so unable to
// close over it directly — mirrors capacityTextFn's package-level pattern)
// acquire the same node:/llm: pools a task on that backend+model would use,
// even when the verifier's resolved backend differs from the task's own
// worker backend (BL25's "cross-backend independence" design). Set once at
// daemon startup by wireCapacity; nil until then, so callers must check.
// v8.36.9 — prdID is now required so the resulting lease/wait entry is
// correctly attributed to its PRD (previously always blank: found live
// retrying PRD a2833a5e — /api/capacity?prd_id= and the capacity card
// couldn't show this PRD had anything waiting at all).
var verifierCapacityAdmit func(ctx context.Context, backend, model, holder, prdID string) (release func(), err error)

// verifierCapacityTryAdmit (v8.36.9) is verifierCapacityAdmit's
// non-blocking sibling for probing autonomous.verification_backends
// candidates — see its assignment in wireCapacity for why this exists.
var verifierCapacityTryAdmit func(ctx context.Context, backend, model, holder, prdID string) (release func(), ok bool)

// councilCapacityAdmit (v8.38.0, BL390) lets council persona/synthesis
// calls acquire the same node:/llm: pools a PRD task on that
// backend+model would use — mirrors verifierCapacityAdmit exactly. Set
// once at daemon startup by wireCapacity; nil until then (council
// wiring runs earlier in main.go, before wireCapacity — callers resolve
// this var at call time, not at wiring time).
var councilCapacityAdmit func(ctx context.Context, backend, model, holder, prdID string) (release func(), err error)

// wireCapacity creates the admission ledger, connects it to the autonomous
// manager and the REST surface, and starts the sync loop that keeps pool
// limits current with config, compute nodes and LLMs, refreshes GPU
// backpressure, reaps leaked leases and raises long-wait alerts.
func wireCapacity(ctx context.Context, cfg *config.Config, mgr *session.Manager, amgr *autonomous.Manager, httpServer *server.HTTPServer, alertFn func(title, body string)) {
	var gpuMu sync.Mutex
	gpuUtil := map[string]float64{}

	external := func(pool string) int {
		if pool != "host" {
			return 0
		}
		n := 0
		host := mgr.Hostname()
		for _, s := range mgr.ListSessions() {
			if s.Hostname == host && (s.State == session.StateRunning || s.State == session.StateWaitingInput) && (!strings.HasPrefix(s.Name, "autonomous:") || s.Name == "autonomous:decompose") {
				n++
			}
		}
		return n
	}
	backpressure := func(pool string) (bool, string) {
		limit := cfg.Autonomous.CapacityGPUUtilPct
		if limit <= 0 || !strings.HasPrefix(pool, "node:") {
			return false, ""
		}
		gpuMu.Lock()
		u, ok := gpuUtil[strings.TrimPrefix(pool, "node:")]
		gpuMu.Unlock()
		if ok && u >= float64(limit) {
			return true, fmt.Sprintf("GPU %.0f%% busy (limit %d%%)", u, limit)
		}
		return false, ""
	}
	led := capacity.New(capacity.Options{External: external, Backpressure: backpressure})

	keys := func(backend, model string) ([]string, string) {
		pools := []string{"host"}
		node := ""
		if reg := httpServer.InferenceRegistry(); reg != nil {
			if l, err := reg.Get(backend); err == nil && l != nil {
				pools = append(pools, "llm:"+l.Name)
				if len(l.ComputeNodes) > 0 {
					node = l.ComputeNodes[0]
					pools = append(pools, "node:"+node)
				}
			}
		}
		return pools, node
	}
	amgr.SetCapacity(led, keys)
	capacityTextFn = func() string { return capacity.FormatText(led.Snapshot()) }
	httpServer.SetCapacity(led)

	// v8.36.0 — interactive (non-autonomous) session starts also admit on
	// the same node:/llm: pools an autonomous task would use for the same
	// backend+model. See handleStartSession's req.OneShot check for why
	// autonomous spawns skip this (they already admit via m.admit()).
	httpServer.SetCapacityAdmit(func(ctx context.Context, backend, model, holder string) error {
		pools, node := keys(backend, model)
		if len(pools) == 0 {
			return nil
		}
		return led.Acquire(ctx, capacity.Request{Holder: holder, Pools: pools, Node: node, Model: model}, 0, func() bool { return false }, func(string) {})
	})
	httpServer.SetCapacityBind(func(holder, sessionID string) { led.Bind(holder, sessionID) })
	httpServer.SetCapacityRelease(func(holder string) { led.Release(holder) })

	verifierCapacityAdmit = func(ctx context.Context, backend, model, holder, prdID string) (func(), error) {
		pools, node := keys(backend, model)
		if len(pools) == 0 {
			return func() {}, nil
		}
		waitDur := amgr.CapacityWaitDuration()
		if err := led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model}, waitDur, func() bool { return false }, func(string) {}); err != nil {
			return func() {}, err
		}
		return func() { led.Release(holder) }, nil
	}

	// councilCapacityAdmit (v8.38.0, BL390) — same shape and wait-duration
	// source as verifierCapacityAdmit; council runs are interactive/short
	// like the verifier's own /api/ask call, so reusing
	// autonomous.capacity_wait_seconds avoids a third copy of the same
	// config knob rather than inventing a council-specific one.
	councilCapacityAdmit = func(ctx context.Context, backend, model, holder, prdID string) (func(), error) {
		pools, node := keys(backend, model)
		if len(pools) == 0 {
			return func() {}, nil
		}
		waitDur := amgr.CapacityWaitDuration()
		if err := led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model}, waitDur, func() bool { return false }, func(string) {}); err != nil {
			return func() {}, err
		}
		return func() { led.Release(holder) }, nil
	}

	// verifierCapacityTryAdmit (v8.36.9) is verifierCapacityAdmit's
	// non-blocking sibling — used to probe a candidate in
	// autonomous.verification_backends without committing to its
	// normal (potentially hours-long) wait. A short, real Acquire call
	// rather than a Snapshot-then-Acquire check: Acquire's own fast
	// path already grants immediately when a pool has room, so this
	// just bounds how long a genuinely-busy candidate is given before
	// the caller moves on to the next one.
	verifierCapacityTryAdmit = func(ctx context.Context, backend, model, holder, prdID string) (func(), bool) {
		pools, node := keys(backend, model)
		if len(pools) == 0 {
			return func() {}, true
		}
		if err := led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model}, 150*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
			return func() {}, false
		}
		return func() { led.Release(holder) }, true
	}

	syncPools := func() {
		max := cfg.Session.MaxSessions
		if max > 0 {
			led.SetLimit("host", max-cfg.Session.EffectiveReservedInteractive())
		} else {
			led.SetLimit("host", 0)
		}
		if reg := httpServer.ComputeRegistry(); reg != nil {
			for _, n := range reg.List() {
				led.SetLimit("node:"+n.Name, n.MaxConcurrentSessions)
				if cfg.Autonomous.CapacityGPUUtilPct > 0 {
					if u, ok := httpServer.NodeGPUStats(n.Name); ok {
						gpuMu.Lock()
						gpuUtil[n.Name] = u
						gpuMu.Unlock()
					}
				}
			}
		}
		if reg := httpServer.InferenceRegistry(); reg != nil {
			for _, l := range reg.List() {
				led.SetLimit("llm:"+l.Name, l.MaxInflight)
			}
		}
		live := func(ls capacity.Lease) bool {
			if ls.SessionID == "" {
				return time.Since(ls.Acquired) < 10*time.Minute // spawn in progress
			}
			s, ok := mgr.GetSession(ls.SessionID)
			return ok && s.State != session.StateComplete && s.State != session.StateFailed && s.State != session.StateKilled
		}
		for _, h := range led.Reap(live) {
			fmt.Printf("[capacity] reaped leaked lease holder=%s\n", h)
			metrics.CapacityLeasesReaped.Inc()
		}
		st := led.Snapshot()
		metrics.CapacityWaiting.Set(float64(len(st.Waiting)))
		for _, p := range st.Pools {
			metrics.CapacityHeld.WithLabelValues(p.Name).Set(float64(p.Held + p.External))
			metrics.CapacityLimit.WithLabelValues(p.Name).Set(float64(p.Limit))
		}
	}
	syncPools()
	go func() {
		alerted := map[string]bool{}
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				syncPools()
				threshold := 30 * time.Minute
				seen := map[string]bool{}
				for _, w := range led.Snapshot().Waiting {
					seen[w.Holder] = true
					if time.Since(w.Since) >= threshold && !alerted[w.Holder] {
						alerted[w.Holder] = true
						alertFn("Automata waiting for capacity", fmt.Sprintf("Task %s (PRD %s) has waited %s: %s", w.Holder, w.PRDID, time.Since(w.Since).Round(time.Minute), w.Reason))
					}
				}
				for h := range alerted {
					if !seen[h] {
						delete(alerted, h)
					}
				}
			}
		}
	}()
}
