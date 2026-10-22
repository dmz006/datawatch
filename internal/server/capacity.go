package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

// SetCapacity wires the admission ledger exposed at GET /api/capacity.
func (s *Server) SetCapacity(l *capacity.Ledger) { s.capacityLedger = l }

// SetCapacity wires the admission ledger on the HTTP server.
func (s *HTTPServer) SetCapacity(l *capacity.Ledger) {
	if s.api != nil {
		s.api.SetCapacity(l)
	}
}

// CapacityAdmitFn blocks (up to a short, caller-chosen timeout) until an
// interactive session start may proceed on the given backend+model's
// node:/llm: pools, or returns an error (typically capacity.ErrWaitTimeout)
// if it can't. holder is a caller-generated key for this admission attempt;
// on success the caller must, once the real session ID is known, bind it via
// the same ledger (see wireCapacity's admitInteractive closure) so the
// existing Reap() cycle in capacity_wiring.go's syncPools releases it
// automatically once the session ends — no separate release call needed.
type CapacityAdmitFn func(ctx context.Context, backend, model, holder string) error

// SetCapacityAdmit wires interactive-session capacity gating (handleStartSession).
func (s *Server) SetCapacityAdmit(fn CapacityAdmitFn) { s.capacityAdmit = fn }

// SetCapacityAdmit wires interactive-session capacity gating on the HTTP server.
func (s *HTTPServer) SetCapacityAdmit(fn CapacityAdmitFn) {
	if s.api != nil {
		s.api.SetCapacityAdmit(fn)
	}
}

// CapacityBindFn attaches a real session ID to a previously-admitted
// interactive holder key, so the ledger's Reap() can find and release it
// once the session ends. No-op (never called) when capacity isn't wired.
type CapacityBindFn func(holder, sessionID string)

// SetCapacityBind wires the bind-after-start step for interactive sessions.
func (s *Server) SetCapacityBind(fn CapacityBindFn) { s.capacityBind = fn }

// SetCapacityBind wires the bind-after-start step on the HTTP server.
func (s *HTTPServer) SetCapacityBind(fn CapacityBindFn) {
	if s.api != nil {
		s.api.SetCapacityBind(fn)
	}
}

// CapacityReleaseFn releases a previously-admitted interactive holder that
// never reached a bound session (e.g. mgr.Start itself failed after
// admission succeeded) — the reaper's <10-minute unbound-lease grace period
// would eventually free it anyway, but releasing explicitly on a known
// failure avoids holding a slot open needlessly.
type CapacityReleaseFn func(holder string)

// SetCapacityRelease wires the release-on-start-failure step.
func (s *Server) SetCapacityRelease(fn CapacityReleaseFn) { s.capacityRelease = fn }

// SetCapacityRelease wires the release-on-start-failure step on the HTTP server.
func (s *HTTPServer) SetCapacityRelease(fn CapacityReleaseFn) {
	if s.api != nil {
		s.api.SetCapacityRelease(fn)
	}
}

// InferenceRegistry returns the wired LLM registry (nil when not set).
func (s *HTTPServer) InferenceRegistry() *inference.Registry {
	if s.api == nil {
		return nil
	}
	return s.api.inferenceReg
}

// NodeGPUStats returns the busiest GPU's utilisation (percent) reported for a
// compute node, and whether any GPU data was available.
func (s *HTTPServer) NodeGPUStats(name string) (maxUtilPct float64, ok bool) {
	if s.api == nil || s.api.computeReg == nil {
		return 0, false
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/compute/nodes/"+name+"/detail", nil)
	s.api.handleComputeNodeDetail(rec, req, name)
	if rec.Code != http.StatusOK {
		return 0, false
	}
	var d struct {
		GPU []struct {
			UtilPct float64 `json:"util_pct"`
		} `json:"gpu"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &d) != nil || len(d.GPU) == 0 {
		return 0, false
	}
	for _, g := range d.GPU {
		if g.UtilPct > maxUtilPct {
			maxUtilPct = g.UtilPct
		}
	}
	return maxUtilPct, true
}

// resolvePoolsForBackend mirrors capacity_wiring.go's keys() closure (which
// lives in cmd/datawatch and isn't reachable from here): the llm:/node:
// pools a task or session on this named backend would actually admit
// against. Named-LLM lookup only — a bare kind string like "claude-code"
// with no matching registry entry correctly resolves to no llm:/node: pool
// (just the implicit "host" pool every backend uses).
func (s *Server) resolvePoolsForBackend(backend string) []string {
	if backend == "" || s.inferenceReg == nil {
		return nil
	}
	l, err := s.inferenceReg.Get(backend)
	if err != nil || l == nil {
		return nil
	}
	pools := []string{"llm:" + l.Name}
	if len(l.ComputeNodes) > 0 {
		pools = append(pools, "node:"+l.ComputeNodes[0])
	}
	return pools
}

// prdRelevantPools returns the set of pool names (always including "host")
// that prdID's own backend, plus any per-story/per-task backend override,
// actually resolves to — so a PRD's capacity card can be scoped to its own
// usage instead of the whole machine's ledger. ok is false when prdID isn't
// found or the autonomous subsystem isn't wired.
func (s *Server) prdRelevantPools(prdID string) (relevant map[string]bool, ok bool) {
	if s.autonomousMgr == nil {
		return nil, false
	}
	raw, found := s.autonomousMgr.GetPRD(prdID)
	if !found {
		return nil, false
	}
	prdMap, isMap := raw.(map[string]any)
	if !isMap {
		return nil, false
	}
	relevant = map[string]bool{"host": true}
	addBackend := func(b string) {
		for _, p := range s.resolvePoolsForBackend(b) {
			relevant[p] = true
		}
	}
	if b, _ := prdMap["backend"].(string); b != "" {
		addBackend(b)
	}
	if stories, ok := prdMap["stories"].([]any); ok {
		for _, sRaw := range stories {
			story, isMap := sRaw.(map[string]any)
			if !isMap {
				continue
			}
			if b, _ := story["backend"].(string); b != "" {
				addBackend(b)
			}
			tasks, ok := story["tasks"].([]any)
			if !ok {
				continue
			}
			for _, tRaw := range tasks {
				task, isMap := tRaw.(map[string]any)
				if !isMap {
					continue
				}
				if b, _ := task["backend"].(string); b != "" {
					addBackend(b)
				}
			}
		}
	}
	return relevant, true
}

// capacityPoolJSON is one pool in the /api/capacity response. queue_depth and
// holder are only set on node:<name> pools; they are pointers so every node
// pool reports them (zero / empty when idle) while other pools keep the
// plain PoolStatus shape.
type capacityPoolJSON struct {
	capacity.PoolStatus
	QueueDepth *int    `json:"queue_depth,omitempty"`
	Holder     *string `json:"holder,omitempty"`
	// For llm:<name> pools: the effective inflight limit and the rule that
	// produced it ("explicit" | "plan_tier" | "kind") — the same
	// inference.EffectiveMaxInflight the daemon's capacity wiring enforces,
	// so the value shown here IS the value the ledger admits against.
	// Nil on non-LLM pools (omitted from JSON). A present 0 means
	// "unlimited by design" (local/API kinds or an explicit operator 0).
	EffectiveMaxInflight *int    `json:"effective_max_inflight,omitempty"`
	InflightSource       *string `json:"inflight_source,omitempty"`
}

// capacityResponse is the /api/capacity body.
type capacityResponse struct {
	Pools   []capacityPoolJSON `json:"pools"`
	Leases  []capacity.Lease   `json:"leases"`
	Waiting []capacity.Waiter  `json:"waiting"`
	// Warnings are model-to-node collisions from the live config (see
	// modelNodeWarnings). Omitted when there are none.
	Warnings []inference.ModelNodeConflict `json:"warnings,omitempty"`
	// Backoff lists the live per-backend usage back-off flags that gate
	// autonomous dispatch (BL428 Phase 4). Always an array, empty when none.
	Backoff []capacityBackoffJSON `json:"backoff"`
	// PatternsVersion / PatternsStale mirror the usage back-off status: the
	// rate-limit pattern fingerprint and whether an operator has rechecked it.
	PatternsVersion string `json:"patterns_version"`
	PatternsStale   bool   `json:"patterns_stale"`
}

// capacityBackoffJSON is one active usage back-off flag in the capacity
// response.
type capacityBackoffJSON struct {
	Backend string    `json:"backend"`
	Until   time.Time `json:"until"`
	Reason  string    `json:"reason"`
}

// capacityBackoff snapshots the live usage back-off flags for the capacity
// response (empty, never nil, when none are wired or active).
func (s *Server) capacityBackoff() []capacityBackoffJSON {
	out := []capacityBackoffJSON{}
	if s.usageBackoff == nil {
		return out
	}
	for _, e := range s.usageBackoff.Live() {
		out = append(out, capacityBackoffJSON{Backend: e.Backend, Until: e.Until, Reason: e.Reason})
	}
	return out
}

// handleCapacity serves GET /api/capacity: pools, limits, holders and the
// wait queue. With ?prd_id=, scopes the pool list to that PRD's own backend
// usage (always includes "host") instead of the whole machine's ledger —
// otherwise a PRD's capacity card showed unrelated nodes/LLMs any other
// PRD or task happened to be using. Every registered compute node is
// always included (even with no configured limit and no current activity)
// so real hardware isn't invisible just because nobody capped it.
func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	warnings := s.modelNodeWarnings()
	if s.capacityLedger == nil {
		empty := capacityResponse{Pools: []capacityPoolJSON{}, Leases: []capacity.Lease{}, Waiting: []capacity.Waiter{}, Warnings: warnings, Backoff: s.capacityBackoff()}
		empty.PatternsVersion, empty.PatternsStale = s.rateLimitPatternsStatus()
		writeJSONOK(w, empty)
		return
	}
	st := s.capacityLedger.Snapshot()
	if st.Pools == nil {
		st.Pools = []capacity.PoolStatus{}
	}
	if st.Leases == nil {
		st.Leases = []capacity.Lease{}
	}
	if st.Waiting == nil {
		st.Waiting = []capacity.Waiter{}
	}

	// Waiters per pool, counted over the whole ledger before any ?prd_id=
	// scoping so a node's queue_depth is the real pool depth.
	waitersOn := map[string]int{}
	for _, wtr := range st.Waiting {
		for _, p := range wtr.Pools {
			waitersOn[p]++
		}
	}

	have := map[string]bool{}
	for _, p := range st.Pools {
		have[p.Name] = true
	}
	if s.computeReg != nil {
		for _, n := range s.computeReg.List() {
			if !have["node:"+n.Name] {
				st.Pools = append(st.Pools, capacity.PoolStatus{Name: "node:" + n.Name})
				have["node:"+n.Name] = true
			}
		}
	}

	if prdID := r.URL.Query().Get("prd_id"); prdID != "" {
		relevant, ok := s.prdRelevantPools(prdID)
		if ok {
			filtered := st.Pools[:0]
			for _, p := range st.Pools {
				if relevant[p.Name] {
					filtered = append(filtered, p)
				}
			}
			st.Pools = filtered
		}
		// v8.36.9 — Leases/Waiting used to be returned unfiltered even when
		// scoping by prd_id, so a PRD's own capacity card either showed
		// every other PRD's leases/waits too, or (for entries the ledger
		// never tagged with a PRDID at all — e.g. the verifier's own
		// capacity request before this release) showed nothing for a PRD
		// that genuinely had something waiting. Now that every autonomous
		// capacity request sets PRDID, filter both to this PRD's own.
		leasesFiltered := st.Leases[:0]
		for _, l := range st.Leases {
			if l.PRDID == prdID {
				leasesFiltered = append(leasesFiltered, l)
			}
		}
		st.Leases = leasesFiltered
		waitingFiltered := st.Waiting[:0]
		for _, wtr := range st.Waiting {
			if wtr.PRDID == prdID {
				waitingFiltered = append(waitingFiltered, wtr)
			}
		}
		st.Waiting = waitingFiltered
	}

	sort.Slice(st.Pools, func(i, j int) bool { return st.Pools[i].Name < st.Pools[j].Name })

	out := capacityResponse{Pools: make([]capacityPoolJSON, 0, len(st.Pools)), Leases: st.Leases, Waiting: st.Waiting, Warnings: warnings, Backoff: s.capacityBackoff()}
	out.PatternsVersion, out.PatternsStale = s.rateLimitPatternsStatus()
	for _, p := range st.Pools {
		pj := capacityPoolJSON{PoolStatus: p}
		if strings.HasPrefix(p.Name, "node:") {
			depth := waitersOn[p.Name]
			holder := strings.Join(p.Holders, ",")
			pj.QueueDepth = &depth
			pj.Holder = &holder
		}
		if llmName := strings.TrimPrefix(p.Name, "llm:"); strings.HasPrefix(p.Name, "llm:") && s.inferenceReg != nil {
			if l, err := s.inferenceReg.Get(llmName); err == nil && l != nil {
				var table map[string]int
				if s.cfg != nil {
					table = s.cfg.Autonomous.PlanTierMaxInflight
				}
				effective := inference.EffectiveMaxInflight(*l, table)
				source := string(inference.InflightSourceOf(*l, table))
				pj.EffectiveMaxInflight = &effective
				pj.InflightSource = &source
			}
		}
		out.Pools = append(out.Pools, pj)
	}
	writeJSONOK(w, out)
}
