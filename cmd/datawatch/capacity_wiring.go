package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/server/multiserver"
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
//
// taskID drives a capacity-lease hand-off (capacity lease hand-off):
// when the verifier's backend+model resolves to exactly the pool set the
// task's worker lease already covers, the worker lease is re-keyed to
// "verify:<taskID>" atomically, so a task in verification holds exactly one
// slot instead of two. Any other situation (different/zero pools, or no
// worker lease left) falls back to the separate acquire — releasing the
// task's worker lease FIRST, so the verifier can never wait behind, or
// double-book with, its own task's slot.
var verifierCapacityAdmit func(ctx context.Context, backend, model, taskID, prdID string) (release func(), err error)

// verifierCapacityTryAdmit (v8.36.9) is verifierCapacityAdmit's
// non-blocking sibling for probing autonomous.verification_backends
// candidates — see its assignment in wireCapacity for why this exists.
var verifierCapacityTryAdmit func(ctx context.Context, backend, model, taskID, prdID string) (release func(), ok bool)

// councilCapacityAdmit (v8.38.0, BL390) lets council persona/synthesis
// calls acquire the same node:/llm: pools a PRD task on that
// backend+model would use — mirrors verifierCapacityAdmit exactly. Set
// once at daemon startup by wireCapacity; nil until then (council
// wiring runs earlier in main.go, before wireCapacity — callers resolve
// this var at call time, not at wiring time).
var councilCapacityAdmit func(ctx context.Context, backend, model, holder, prdID string) (release func(), err error)

// engineNodes maps each inference LLM kind (the provider prefix of a model
// such as "ollama/qwen3.8:27b") to the compute node that hosts it: the first
// ComputeNode of the first LLM of that kind. Kinds with no compute node are
// absent, so autonomous.LocalEngineGate leaves them ungated. A nil registry
// yields an empty map.
func engineNodes(reg *inference.Registry) map[string]string {
	nodes := map[string]string{}
	if reg == nil {
		return nodes
	}
	for _, l := range reg.List() {
		kind := string(l.Kind)
		if _, seen := nodes[kind]; seen || len(l.ComputeNodes) == 0 {
			continue
		}
		nodes[kind] = l.ComputeNodes[0]
	}
	return nodes
}

// syncLLMLimits sets the ledger limit for every LLM entry's pool
// ("llm:<name>") from inference.EffectiveMaxInflight: the explicit
// max_inflight override when > 0, else the autonomous.plan_tier_max_inflight
// tier table, else the conservative kind-based default (1 for subscription
// kinds like claude-code; 0/unlimited for local Ollama-protocol kinds, which
// stay governed by the node: pools and the two-engines-per-GPU limit above).
// Extracted from syncPools so it is unit-testable without a live daemon; the
// 10s syncPools ticker re-runs it on registry change.
func syncLLMLimits(cfg *config.Config, reg *inference.Registry, led *capacity.Ledger) {
	for _, l := range reg.List() {
		led.SetLimit("llm:"+l.Name, inference.EffectiveMaxInflight(*l, cfg.Autonomous.PlanTierMaxInflight))
	}
}

// backoffBackpressure reports an "llm:<backend>" pool as blocked while the
// shared autonomous back-off registry holds an active flag for that backend,
// so task, verifier and decompose admission wait in the capacity queue with
// the back-off as their wait reason. Only llm: pools are gated — host, node:
// and external pools are never blocked here. It is installed as the ledger's
// AutonomousBackpressure, which requests marked Interactive skip, so operator
// session starts are never held back by a back-off.
func backoffBackpressure(bo *autonomous.Backoff) func(pool string) (bool, string) {
	return func(pool string) (bool, string) {
		backend, ok := strings.CutPrefix(pool, "llm:")
		if !ok || bo == nil {
			return false, ""
		}
		if blocked, why := bo.Blocked(backend); blocked {
			return true, "backoff: " + why
		}
		return false, ""
	}
}

// budgetBackpressure reports an "llm:<name>" pool as blocked while that LLM's
// dollar budget window is full, with the ledger's "budget window full: ..."
// reason as the wait reason. Like backoffBackpressure it gates only llm:
// pools — host, node: and external pools are never blocked — and it is
// consulted only for autonomous requests, so operator session starts are
// never held back. Admission resumes on the wait loop's next poll once the
// window rolls over.
func budgetBackpressure(bl *autonomous.BudgetLedger) func(pool string) (bool, string) {
	return func(pool string) (bool, string) {
		name, ok := strings.CutPrefix(pool, "llm:")
		if !ok || bl == nil {
			return false, ""
		}
		return bl.Blocked(name)
	}
}

// sharedNodeBackpressure reports a "node:<name>" pool as blocked while the
// fingerprint gate (wired by wireFingerprint, see
// cmd/datawatch/fingerprint_wiring.go) says a CONFIRMED shared-node match
// means the peer's concurrent usage of the same underlying node pushes the
// pool past its local limit. Like the other backpressure gates it is
// consulted by the ledger only for autonomous requests — operator starts
// are never held — and for no pool kind other than node: (host, llm: and
// external pools are never blocked by it). The gate itself fails open:
// with no opted-in peer, no confirmed match, an unreachable peer, or no
// gate wired at all, it reports not blocked and admission proceeds exactly
// as if the fingerprint feature did not exist.
func sharedNodeBackpressure() func(pool string) (bool, string) {
	return func(pool string) (bool, string) {
		node, ok := strings.CutPrefix(pool, "node:")
		if !ok || fingerprintPeerGate == nil {
			return false, ""
		}
		if why := fingerprintPeerGate.Reason(node); why != "" {
			return true, "shared node: " + why
		}
		return false, ""
	}
}

// composeBackpressure returns a gate that answers with the first blocked
// answer of fns, in order, and reports not blocked when none block. Nil
// functions are skipped.
func composeBackpressure(fns ...func(pool string) (bool, string)) func(pool string) (bool, string) {
	return func(pool string) (bool, string) {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			if blocked, why := fn(pool); blocked {
				return true, why
			}
		}
		return false, ""
	}
}

// newAutonomousBudget builds the budget ledger persisting under dataDir. Its
// budget, plan-tier and allocation sources read the live inference registry
// and config at call time, so edits apply without a restart.
func newAutonomousBudget(cfg *config.Config, dataDir string, registry func() *inference.Registry) *autonomous.BudgetLedger {
	bl := autonomous.NewBudgetLedger(filepath.Join(dataDir, "autonomous", "budget-ledger.jsonl"))
	lookup := func(llm string) *inference.LLM {
		reg := registry()
		if reg == nil {
			return nil
		}
		l, err := reg.Get(llm)
		if err != nil {
			return nil
		}
		return l
	}
	bl.Budgets = func(llm string) *inference.BudgetWindow {
		if l := lookup(llm); l != nil {
			return l.Budget
		}
		return nil
	}
	bl.PlanTier = func(llm string) inference.PlanTier {
		if l := lookup(llm); l != nil {
			return l.PlanTier
		}
		return inference.PlanTierUnset
	}
	bl.PlanTierAllocations = func() map[string]float64 { return cfg.Autonomous.PlanTierAllocationUSD }
	bl.LLMs = func() []string {
		reg := registry()
		if reg == nil {
			return nil
		}
		var names []string
		for _, l := range reg.List() {
			names = append(names, l.Name)
		}
		return names
	}
	return bl
}

// newPeerBudget builds the peer-spend cache for the budget ledger. Peers are
// resolved through the live multiserver store at call time and counted only
// when the LLM's peer_window lists them (the default confirmer), so no peer
// is contacted unless the operator opted it in.
func newPeerBudget(registry func() *inference.Registry, store func() *multiserver.Store) *autonomous.PeerBudget {
	lookup := func(llm string) *inference.LLM {
		reg := registry()
		if reg == nil {
			return nil
		}
		l, err := reg.Get(llm)
		if err != nil {
			return nil
		}
		return l
	}
	pb := &autonomous.PeerBudget{}
	pb.Budgets = func(llm string) *inference.BudgetWindow {
		if l := lookup(llm); l != nil {
			return l.Budget
		}
		return nil
	}
	pb.LLMs = func() []string {
		reg := registry()
		if reg == nil {
			return nil
		}
		var names []string
		for _, l := range reg.List() {
			names = append(names, l.Name)
		}
		return names
	}
	pb.Lookup = func(name string) (string, string, bool) {
		st := store()
		if st == nil {
			return "", "", false
		}
		return st.GetByName(name)
	}
	pb.KnownPeers = func() []string {
		st := store()
		if st == nil {
			return nil
		}
		var names []string
		for _, e := range st.List() {
			names = append(names, e.Name)
		}
		return names
	}
	return pb
}

// recordUsageSpend returns the session manager's usage listener: it records
// every event with a positive dollar amount under the LLM the backend
// resolves to in the inference registry (the backend name itself when it has
// no entry), whether or not that LLM has a budget yet, so history exists when
// a budget is switched on later.
func recordUsageSpend(bl *autonomous.BudgetLedger, registry func() *inference.Registry) func(session.UsageEvent) {
	return func(ev session.UsageEvent) {
		if bl == nil || ev.USD <= 0 {
			return
		}
		bl.RecordSession(budgetLLMName(registry, ev.Backend), ev.SessionID, ev.At, ev.USD, ev.TokensIn, ev.TokensOut)
	}
}

// budgetLLMName is the LLM a backend's spend is recorded under: the inference
// registry's name for it, or the backend name itself when it has no entry.
func budgetLLMName(registry func() *inference.Registry, backend string) string {
	if reg := registry(); reg != nil {
		if l, err := reg.Get(backend); err == nil && l != nil {
			return l.Name
		}
	}
	return backend
}

// sessionCostTotal returns the per-session lifetime cost source for the
// autonomous manager: the session's estimated cost (the figure
// session.SummaryFor sums) and the LLM name its spend is recorded under.
func sessionCostTotal(get func(id string) (*session.Session, bool), registry func() *inference.Registry) func(string) (float64, string, bool) {
	return func(id string) (float64, string, bool) {
		s, ok := get(id)
		if !ok || s == nil {
			return 0, "", false
		}
		return s.EstCostUSD, budgetLLMName(registry, s.BackendFamily), true
	}
}

// auditBudgetTransitions compares each budgeted LLM's full state with the
// state seen on the previous call (full, updated in place) and emits one
// audit event per change: ActionBudgetCeilingWait when a window fills,
// ActionBudgetWindowResumed when it stops being full or the LLM loses its
// budget. Unchanged state emits nothing.
func auditBudgetTransitions(bl *autonomous.BudgetLedger, full map[string]bool, audit backoffAuditFunc) {
	if bl == nil {
		return
	}
	seen := map[string]bool{}
	for _, st := range bl.StatusAll() {
		seen[st.LLM] = true
		if st.Full == full[st.LLM] {
			continue
		}
		full[st.LLM] = st.Full
		details := map[string]any{
			"llm":         st.LLM,
			"spend_usd":   st.SpendUSD + st.PeerSpendUSD,
			"ceiling_usd": st.CeilingUSD,
		}
		if st.Full {
			if st.RollsOverAt != nil {
				details["rolls_over_at"] = st.RollsOverAt.UTC().Format(time.RFC3339)
			}
			audit(auditpkg.ActionBudgetCeilingWait, details)
			continue
		}
		audit(auditpkg.ActionBudgetWindowResumed, details)
	}
	var gone []string
	for llm, wasFull := range full {
		if !seen[llm] {
			if wasFull {
				gone = append(gone, llm)
			}
			delete(full, llm)
		}
	}
	sort.Strings(gone)
	for _, llm := range gone {
		audit(auditpkg.ActionBudgetWindowResumed, map[string]any{"llm": llm})
	}
}

// wireCapacity creates the admission ledger, connects it to the autonomous
// manager and the REST surface, and starts the sync loop that keeps pool
// limits current with config, compute nodes and LLMs, refreshes GPU
// backpressure, reaps leaked leases and raises long-wait alerts. It returns
// the ledger so the pin keepers can tell when a node is in use.
func wireCapacity(ctx context.Context, cfg *config.Config, mgr *session.Manager, amgr *autonomous.Manager, httpServer *server.HTTPServer, alertFn func(title, body string)) *capacity.Ledger {
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
	// A spawn refused by the BL30 global cooldown waits instead of failing;
	// the reset time words the task's wait reason.
	amgr.SetCooldownUntilFn(func() time.Time { return mgr.CooldownStatus().Until })
	registry := httpServer.InferenceRegistry
	autonomousBudget = newAutonomousBudget(cfg, expandHome(cfg.DataDir), registry)
	mgr.SetUsageEventHook(recordUsageSpend(autonomousBudget, registry))
	amgr.SetCostSources(autonomousBudget, sessionCostTotal(mgr.GetSession, registry))
	peerBudget := newPeerBudget(registry, httpServer.ServerStore)
	automationBudget := autonomous.NewAutomationBudget(amgr, autonomousBudget)
	automationBudget.Backoffs = autonomousBackoff.Live
	automationBudget.Peers = peerBudget
	automationBudget.Resets = automationBudget.ResetFor
	amgr.SetAutomationBudget(automationBudget)
	autonomousBudget.PeerSpend = peerBudget.Spend
	autonomousBudget.PeerBreakdown = peerBudget.Breakdown
	led := capacity.New(capacity.Options{
		External:     external,
		Backpressure: backpressure,
		AutonomousBackpressure: composeBackpressure(
			backoffBackpressure(autonomousBackoff),
			budgetBackpressure(autonomousBudget),
			sharedNodeBackpressure(),
		),
	})

	keys := func(backend, model string) ([]string, string) {
		pools := []string{"host"}
		reg := httpServer.InferenceRegistry()
		if reg != nil {
			if l, err := reg.Get(backend); err == nil && l != nil {
				pools = append(pools, "llm:"+l.Name)
			}
		}
		// Only a local-engine session takes a compute node's pool. A
		// hosted-backend session gets no node pool and no node affinity.
		node, gated := autonomous.LocalEngineGate(backend, model, func(provider string) (string, bool) {
			n, ok := engineNodes(reg)[provider]
			return n, ok
		})
		if gated {
			pools = append(pools, "node:"+node)
		}
		return pools, node
	}
	amgr.SetCapacity(led, keys)
	amgr.SetCapacityAudit(capacityAudit)
	amgr.SetBackoff(autonomousBackoff)
	amgr.SetPatternsStatus(mgr.RateLimitGuardStatus)
	// Seed the claude CLI version cache once at daemon start so the first
	// status read has the tool text without waiting on it: a slow or missing
	// tool must never delay startup, so it runs off the hot path.
	go func() { _ = mgr.RefreshClaudeCLIVersion() }()
	amgr.SetBudgetsStatus(autonomousBudget.StatusAll)
	httpServer.SetUsageBackoff(autonomousBackoff)
	httpServer.SetBudgetLedger(autonomousBudget)
	// The in-process text path resolves every holder and the holders named in
	// each wait reason through the shared autonomous resolver when it is wired
	// (amgr), so the comm `capacity` reply (and the session capacity command)
	// shows readable holder names with short ids instead of raw ids: the
	// resolved forms are folded into the snapshot's display fields, which
	// capacity.FormatText prefers over the raw ids (nil amgr keeps the raw ids).
	capacityTextFn = func() string {
		st := led.Snapshot()
		if amgr == nil {
			return capacity.FormatText(st)
		}
		for i := range st.Pools {
			if len(st.Pools[i].Holders) == 0 {
				continue
			}
			names := make([]string, 0, len(st.Pools[i].Holders))
			for _, h := range st.Pools[i].Holders {
				names = append(names, autonomous.DisplayHolderName(amgr.ResolveHolder(h)))
			}
			st.Pools[i].HoldNames = names
		}
		for i := range st.Waiting {
			st.Waiting[i].HoldName = autonomous.DisplayHolderName(amgr.ResolveHolder(st.Waiting[i].Holder))
			if r := amgr.ReadableWaitSentence(st.Waiting[i].Reason); r != st.Waiting[i].Reason {
				st.Waiting[i].ReadReason = r
			}
		}
		return capacity.FormatText(st)
	}
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
		return led.Acquire(ctx, capacity.Request{Holder: holder, Pools: pools, Node: node, Model: model, Interactive: true}, 0, func() bool { return false }, func(string) {})
	})
	httpServer.SetCapacityBind(func(holder, sessionID string) { led.Bind(holder, sessionID) })
	httpServer.SetCapacityRelease(func(holder string) { led.Release(holder) })

	// verifyHandoff re-keys the task's worker lease (holder = the task id) to
	// the verifier's "verify:<taskID>" lease when both resolve to the same
	// pool set: one slot for the task from spawn through verification, with
	// no release-and-re-wait window in which a sibling task can steal the
	// slot. False means "no hand-off possible" and the caller must fall back
	// to the separate acquire (releasing the worker lease first — no
	// stacking, no deadlock).
	verifyHandoff := func(taskID, backend, model string) bool {
		return amgr.HandoffVerifyLease(taskID, backend, model)
	}

	verifierCapacityAdmit = func(ctx context.Context, backend, model, taskID, prdID string) (func(), error) {
		holder := autonomous.VerifyHolder(taskID)
		if verifyHandoff(taskID, backend, model) {
			// The worker's slot is now the verifier's lease: release it by
			// its new name; the executor's deferred release of the worker
			// lease is a harmless no-op.
			return func() { led.Release(holder) }, nil
		}
		// Fallback: the verifier resolves elsewhere (or the worker lease is
		// already gone). Release the worker's slot first so it can neither
		// wait behind its own task's lease nor hold two.
		led.Release(taskID)
		pools, node := keys(backend, model)
		if len(pools) == 0 {
			return func() {}, nil
		}
		waitDur := amgr.CapacityWaitDuration()
		if err := led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model, Role: capacity.RoleVerifier}, waitDur, func() bool { return false }, func(string) {}); err != nil {
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
	verifierCapacityTryAdmit = func(ctx context.Context, backend, model, taskID, prdID string) (func(), bool) {
		holder := autonomous.VerifyHolder(taskID)
		if verifyHandoff(taskID, backend, model) {
			return func() { led.Release(holder) }, true
		}
		if pools, node := keys(backend, model); len(pools) > 0 {
			// Fallback probe. The worker lease is released first (idempotent)
			// so this candidate either takes its own slot or does not — never
			// a second slot on top of the worker's.
			led.Release(taskID)
			if err := led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model, Role: capacity.RoleVerifier}, 150*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
				return func() {}, false
			}
			return func() { led.Release(holder) }, true
		}
		return func() {}, true
	}

	syncPools := func() {
		max := cfg.Session.MaxSessions
		if max > 0 {
			led.SetLimit("host", max-cfg.Session.EffectiveReservedInteractive())
		} else {
			led.SetLimit("host", 0)
		}
		// A compute node that hosts local engines takes its pool limit from
		// autonomous.max_engines_per_node. The node's own
		// max_concurrent_sessions stays a ceiling when it is set and smaller.
		engines := cfg.Autonomous.MaxEnginesPerNode
		if engines < 1 {
			engines = 1
		}
		hosting := map[string]bool{}
		for _, node := range engineNodes(httpServer.InferenceRegistry()) {
			hosting[node] = true
		}
		if reg := httpServer.ComputeRegistry(); reg != nil {
			for _, n := range reg.List() {
				limit := n.MaxConcurrentSessions
				if hosting[n.Name] {
					limit = engines
					if n.MaxConcurrentSessions > 0 && n.MaxConcurrentSessions < limit {
						limit = n.MaxConcurrentSessions
					}
				}
				led.SetLimit("node:"+n.Name, limit)
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
			syncLLMLimits(cfg, reg, led)
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
		budgetFull := map[string]bool{}
		rollovers := &autonomous.BudgetRolloverWatcher{Audit: capacityAudit}
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				syncPools()
				// Peer spend is fetched off the tick so a slow peer never
				// delays pool syncing; admission reads only the cache.
				go peerBudget.Refresh(ctx)
				auditBudgetTransitions(autonomousBudget, budgetFull, emitBackoffAudit)
				if autonomousBudget != nil {
					rollovers.Observe(autonomousBudget.StatusAll())
				}
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
	return led
}
