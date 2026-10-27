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
	"github.com/dmz006/datawatch/internal/compute"
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
//
// The node return is the compute node the verifier holds: "" unless its LLM
// entry pools two or more nodes, in which case the verifier session or ask
// call must start on that node.
var verifierCapacityAdmit func(ctx context.Context, backend, model, taskID, prdID, sessionID string) (release func(), node string, err error)

// verifierCapacityTryAdmit (v8.36.9) is verifierCapacityAdmit's
// non-blocking sibling for probing autonomous.verification_backends
// candidates — see its assignment in wireCapacity for why this exists.
var verifierCapacityTryAdmit func(ctx context.Context, backend, model, taskID, prdID, sessionID string) (release func(), node string, ok bool)

// councilCapacityAdmit (v8.38.0, BL390) lets council persona/synthesis
// calls acquire the same node:/llm: pools a PRD task on that
// backend+model would use — mirrors verifierCapacityAdmit exactly. Set
// once at daemon startup by wireCapacity; nil until then (council
// wiring runs earlier in main.go, before wireCapacity — callers resolve
// this var at call time, not at wiring time).
var councilCapacityAdmit func(ctx context.Context, backend, model, holder, prdID string) (release func(), err error)

// engineEntry returns the LLM entry that hosts model for the inference LLM
// kind (the provider prefix of a model such as "ollama/qwen3.8:27b"). Only
// entries of that kind that list a compute node are considered. One that names
// the model in its models list beats one that does not; among equals the entry
// whose name sorts first wins, so the answer never depends on registry
// iteration order. The second result is false when no entry of the kind lists a
// compute node, so autonomous.LocalEngineGate leaves the kind ungated. A nil
// registry has none.
func engineEntry(reg *inference.Registry, kind, model string) (inference.LLM, bool) {
	if reg == nil {
		return inference.LLM{}, false
	}
	var best *inference.LLM
	bestListed := false
	for _, l := range reg.List() {
		if string(l.Kind) != kind || len(l.ComputeNodes) == 0 {
			continue
		}
		listed := listsModel(*l, model)
		if best == nil || (listed && !bestListed) || (listed == bestListed && l.Name < best.Name) {
			best, bestListed = l, listed
		}
	}
	if best == nil {
		return inference.LLM{}, false
	}
	return *best, true
}

// piOwnEntry returns the registry entry of a pi backend (pi or pi-prompt) when
// it lists compute nodes of its own, so placement uses that list rather than
// the node list of the provider's engine entry.
func piOwnEntry(reg *inference.Registry, backend string) (inference.LLM, bool) {
	if reg == nil || (backend != "pi" && backend != "pi-prompt") {
		return inference.LLM{}, false
	}
	l, err := reg.Get(backend)
	if err != nil || l == nil || len(l.ComputeNodes) == 0 {
		return inference.LLM{}, false
	}
	return *l, true
}

// listsModel reports whether l's models list names model on any node. model is
// matched as written and without its provider prefix.
func listsModel(l inference.LLM, model string) bool {
	name := nodeModelName(model)
	for _, m := range l.Models {
		if m.Model != "" && (m.Model == name || m.Model == model) {
			return true
		}
	}
	return false
}

// nodeServes reports whether the entry's models list leaves node a candidate
// for model. A node the list pairs with at least one model serves model only if
// one of those is model; a node the list says nothing about is unknown and
// stays a candidate rather than being dropped.
func nodeServes(l inference.LLM, node, model string) bool {
	name := nodeModelName(model)
	listed := false
	for _, m := range l.Models {
		if m.Node != node || m.Model == "" {
			continue
		}
		listed = true
		if m.Model == name || m.Model == model {
			return true
		}
	}
	return !listed
}

// engineNodes maps each inference LLM kind to every compute node of every LLM
// entry of that kind, in entry order and list order with repeats dropped. It
// names the nodes that host an engine at all, whichever entry owns a model.
func engineNodes(reg *inference.Registry) map[string][]string {
	nodes := map[string][]string{}
	if reg == nil {
		return nodes
	}
	seen := map[string]map[string]bool{}
	for _, l := range reg.List() {
		kind := string(l.Kind)
		for _, n := range l.ComputeNodes {
			if n == "" || seen[kind][n] {
				continue
			}
			if seen[kind] == nil {
				seen[kind] = map[string]bool{}
			}
			seen[kind][n] = true
			nodes[kind] = append(nodes[kind], n)
		}
	}
	return nodes
}

// placementSignature summarises what decides where a queued request may run:
// for each LLM entry that lists compute nodes, its kind, its compute_nodes list
// and models list and, per listed node, whether the compute registry knows it
// and at what priority. Two equal signatures mean the resolver would return the
// same candidates, so waiters need no re-plan.
func placementSignature(registry func() *inference.Registry, nodes func() *compute.Registry) string {
	var reg *inference.Registry
	if registry != nil {
		reg = registry()
	}
	priority := computePriority(nodes)
	var b strings.Builder
	if reg != nil {
		for _, l := range reg.List() {
			if len(l.ComputeNodes) == 0 {
				continue
			}
			b.WriteString(l.Name)
			b.WriteByte('/')
			b.WriteString(string(l.Kind))
			b.WriteByte(':')
			for _, n := range l.ComputeNodes {
				p, ok := priority(n)
				fmt.Fprintf(&b, "%s/%d/%t,", n, p, ok)
			}
			b.WriteByte('|')
			for _, m := range l.Models {
				fmt.Fprintf(&b, "%s=%s,", m.Node, m.Model)
			}
			b.WriteByte(';')
		}
	}
	return b.String()
}

// replanOnPlacementChange returns a function that re-plans the ledger's
// waiters when placementSignature differs from the last call's. The first call
// only records the signature.
func replanOnPlacementChange(led *capacity.Ledger, registry func() *inference.Registry, nodes func() *compute.Registry) func() {
	var mu sync.Mutex
	last := placementSignature(registry, nodes)
	return func() {
		sig := placementSignature(registry, nodes)
		mu.Lock()
		changed := sig != last
		last = sig
		mu.Unlock()
		if changed {
			led.Replan()
		}
	}
}

// piDefaultModel returns the lookup the autonomous manager uses to qualify a
// pi-family model that names no provider. It reads the model each pi backend
// launches on when a session names none, the same configured value the
// session manager applies, so the provider chosen for admission is the one
// the launch uses. The config is read on each call, so a config change takes
// effect without a restart. Any other backend, and an unset model, report "".
func piDefaultModel(cfg *config.Config) autonomous.PiDefaultModelFn {
	return func(backend string) string {
		switch backend {
		case "pi-prompt":
			return cfg.PiPrompt.Model
		case "pi":
			return cfg.Pi.Model
		}
		return ""
	}
}

// defaultNodePriority is the scheduling priority of a compute node whose
// record leaves it unset (the compute registry applies the same default).
const defaultNodePriority = 50

// computePriority reads a node's scheduling priority from the compute
// registry; a node the registry does not hold is unknown. A nil registry
// knows no node.
func computePriority(nodes func() *compute.Registry) func(string) (int, bool) {
	return func(name string) (int, bool) {
		var reg *compute.Registry
		if nodes != nil {
			reg = nodes()
		}
		if reg == nil {
			return 0, false
		}
		n, err := reg.Get(name)
		if err != nil || n == nil {
			return 0, false
		}
		if n.SchedulingPriority == 0 {
			return defaultNodePriority, true
		}
		return n.SchedulingPriority, true
	}
}

// hostingEntry returns the LLM entry that hosts a local-engine session of
// backend and model, and that entry's first compute node. gated is false for a
// session that runs on no local engine. A pi backend that lists compute nodes
// of its own places on that list; the engine gate only decides that the
// session is local.
func hostingEntry(reg *inference.Registry, backend, model string) (entry inference.LLM, node string, gated bool) {
	node, gated = autonomous.LocalEngineGate(backend, model, func(provider string) (string, bool) {
		l, ok := engineEntry(reg, provider, model)
		if !ok {
			return "", false
		}
		entry = l
		return l.ComputeNodes[0], true
	})
	if !gated {
		return inference.LLM{}, node, false
	}
	if own, ok := piOwnEntry(reg, backend); ok {
		entry = own
		node = own.ComputeNodes[0]
	}
	return entry, node, true
}

// entryNodeCount builds the counter that tells adoption how many compute nodes
// the entry hosting a backend and model lists, before any serving filter. It
// is 0 for a backend that runs on no local engine.
func entryNodeCount(registry func() *inference.Registry) autonomous.EntryNodesFn {
	return func(backend, model string) int {
		var reg *inference.Registry
		if registry != nil {
			reg = registry()
		}
		entry, _, gated := hostingEntry(reg, backend, model)
		if !gated {
			return 0
		}
		return len(entry.ComputeNodes)
	}
}

// capacityResolver builds the pool resolver the admission paths share. For a
// backend and model it returns the capacity pools, the primary compute node
// and, when the hosting LLM entry has two or more candidate nodes, those
// candidates.
//
// For a local engine the hosting entry is, for a pi backend (pi or pi-prompt)
// whose own entry lists compute_nodes, that backend entry, so its own node list
// is the pool. Any other backend, and a pi backend with no node list of its own,
// uses the entry engineEntry picks for the model's provider: the entry that
// lists the model, ties broken by entry name. Whichever entry is chosen, its
// compute_nodes are the candidates, each kept only if the
// compute registry knows it and the entry's models list does not exclude it for
// this model (a node the list says nothing about stays a candidate). If that
// filter would leave no node, the known nodes are used unfiltered.
//
// A session whose entry yields one node (or one candidate) gets that node's pool
// and node exactly as before. When two or more nodes are candidates the pools
// carry no node:<name> pool and node is empty: the caller puts the candidates on
// its capacity.Request and the ledger picks the node, one engine per node, least
// loaded first with the node's scheduling priority as the tiebreak (the
// candidates carry that priority; the ledger does the ordering). An
// entry none of whose nodes the compute registry knows keeps its first node, as
// it always did. A hosted backend gets no node pool and no node affinity.
func capacityResolver(registry func() *inference.Registry, nodes func() *compute.Registry) func(backend, model string) (pools []string, node string, choices []capacity.NodeChoice) {
	priority := computePriority(nodes)
	return func(backend, model string) ([]string, string, []capacity.NodeChoice) {
		pools := []string{"host"}
		var reg *inference.Registry
		if registry != nil {
			reg = registry()
		}
		if reg != nil {
			if l, err := reg.Get(backend); err == nil && l != nil {
				pools = append(pools, "llm:"+l.Name)
			}
		}
		// Only a local-engine session takes a compute node's pool.
		entry, node, gated := hostingEntry(reg, backend, model)
		if !gated {
			return pools, node, nil
		}
		if len(entry.ComputeNodes) > 1 {
			ordered := inference.OrderedPoolNodes(entry, priority)
			serving := make([]inference.PoolNode, 0, len(ordered))
			for _, n := range ordered {
				if nodeServes(entry, n.Name, model) {
					serving = append(serving, n)
				}
			}
			if len(serving) > 0 {
				ordered = serving
			}
			if len(ordered) >= 2 {
				choices := make([]capacity.NodeChoice, 0, len(ordered))
				for _, n := range ordered {
					choices = append(choices, capacity.NodeChoice{Name: n.Name, Priority: n.Priority})
				}
				return pools, "", choices
			}
			if len(ordered) == 1 {
				node = ordered[0].Name
			}
		}
		return append(pools, "node:"+node), node, nil
	}
}

// nodeLoader is the autonomous.NodeLoader a worker's placement uses on a
// multi-node LLM entry: it finds the leased node in the compute registry and
// makes the model resident there. A node the registry does not hold fails the
// load, so the task fails over to another node of the entry.
//
// The model a lease carries is provider-qualified ("ollama/qwen3.8:27b"); the
// node knows it only by its own name ("qwen3.8:27b"), so the provider prefix
// is dropped before the load request.
func nodeLoader(nodes func() *compute.Registry) autonomous.NodeLoader {
	loader := &inference.ModelLoader{}
	return func(ctx context.Context, name, model string, wait func(reason string)) error {
		var reg *compute.Registry
		if nodes != nil {
			reg = nodes()
		}
		var node *compute.Node
		if reg != nil {
			if n, err := reg.Get(name); err == nil {
				node = n
			}
		}
		if node == nil {
			return &inference.NodeLoadError{Node: name, Model: model, Cause: fmt.Errorf("node %s is not in the compute registry", name)}
		}
		return loader.Ensure(ctx, node, nodeModelName(model), wait)
	}
}

// nodeModelName is the name a compute node serves model under: the text after
// the provider prefix, which ends at the first slash. A model with no prefix
// is returned as written.
func nodeModelName(model string) string {
	if _, name, found := strings.Cut(model, "/"); found && name != "" {
		return name
	}
	return model
}

// capacityManagerKeys splits a resolver into the two closures the autonomous
// manager is wired with: the pool keys (SetCapacity) and the candidate nodes
// (SetCapacityChoices). Both read the same resolver call, so they agree on
// one backend and model.
func capacityManagerKeys(resolve func(backend, model string) (pools []string, node string, choices []capacity.NodeChoice)) (autonomous.CapacityKeysFn, autonomous.CapacityChoicesFn) {
	keys := func(backend, model string) ([]string, string) {
		pools, node, _ := resolve(backend, model)
		return pools, node
	}
	choices := func(backend, model string) []capacity.NodeChoice {
		_, _, c := resolve(backend, model)
		return c
	}
	return keys, choices
}

// resolveRefresh builds a Request.Refresh for a blocking admission: each call
// resolves the backend and model again and returns base with Pools, Node and
// NodeChoices replaced wholesale, so a node the compute registry learns while
// the request waits joins the candidates and a stale node:<name> pool does not
// survive a change from several nodes to one.
func resolveRefresh(resolve func(backend, model string) ([]string, string, []capacity.NodeChoice), base capacity.Request, backend, model string) func() capacity.Request {
	return func() capacity.Request {
		cur := base
		pools, node, choices := resolve(backend, model)
		cur.Pools, cur.Node, cur.NodeChoices = pools, node, nil
		return withNodeChoices(cur, choices)
	}
}

// withNodeChoices puts the candidate nodes on a request; fewer than two leaves
// it unchanged.
func withNodeChoices(req capacity.Request, choices []capacity.NodeChoice) capacity.Request {
	if len(choices) >= 2 {
		req.NodeChoices = choices
	}
	return req
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

// rateWindowDefault is the rolling window used when a rate ceiling is set
// but autonomous.capacity_rate_window_seconds is 0.
const rateWindowDefault = time.Hour

// llmRateCeiling is the rate ceiling autonomous.capacity_rate_* describes:
// the zero value (no ceiling) when both requests and tokens are 0.
func llmRateCeiling(cfg *config.Config) capacity.RateCeiling {
	a := cfg.Autonomous
	if a.CapacityRateRequests <= 0 && a.CapacityRateTokens <= 0 {
		return capacity.RateCeiling{}
	}
	w := time.Duration(a.CapacityRateWindowSeconds) * time.Second
	if w <= 0 {
		w = rateWindowDefault
	}
	return capacity.RateCeiling{Requests: max(a.CapacityRateRequests, 0), Tokens: int64(max(a.CapacityRateTokens, 0)), Window: w}
}

// syncLLMRates applies the configured rate ceiling to every LLM entry's pool
// ("llm:<name>"). It runs on startup and on each syncPools tick, so a config
// change (including clearing the ceiling) takes effect without a restart. An
// unchanged ceiling is left alone so its recorded usage is kept.
func syncLLMRates(cfg *config.Config, reg *inference.Registry, led *capacity.Ledger) {
	want := llmRateCeiling(cfg)
	for _, l := range reg.List() {
		pool := "llm:" + l.Name
		if led.RateOf(pool) != want {
			led.SetRate(pool, want)
		}
	}
}

// backoffBackpressure reports an "llm:<backend>" pool as blocked while the
// shared autonomous back-off registry holds an active flag for that backend,
// so task, verifier and decompose admission wait in the capacity queue with
// the back-off as their wait reason. Only llm: pools are gated — host, node:
// and external pools are never blocked here. It is installed as the ledger's
// AutonomousBackpressure, which requests marked Interactive skip, so operator
// session starts are never held back by a back-off. It runs under the capacity
// ledger lock (rule on capacity.Options): Backoff.Blocked only reads an
// in-memory map under its own short mutex.
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
// window rolls over. It runs under the capacity ledger lock (rule on
// capacity.Options): BudgetLedger.Blocked reads in-memory entries and the
// peer-spend cache only; the budget file is preloaded at startup and its
// writes happen outside the mutex Blocked takes.
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
// as if the fingerprint feature did not exist. It runs under the capacity
// ledger lock (rule on capacity.Options): the gate reads its in-memory view
// of peers and matches and the cached peer usage, never the stores or the
// network, and never the ledger.
func sharedNodeBackpressure() func(pool string, held, limit int) (bool, string) {
	return func(pool string, held, limit int) (bool, string) {
		node, ok := strings.CutPrefix(pool, "node:")
		if !ok || fingerprintPeerGate == nil {
			return false, ""
		}
		// The ledger calls this while holding its own lock, so the held and
		// limit come from the ledger as arguments; reading the ledger back
		// here would deadlock it.
		if why := fingerprintPeerGate.ReasonWithHeld(node, held, limit); why != "" {
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
	return recordUsage(bl, registry, nil)
}

// recordUsage is recordUsageSpend plus the capacity ledger: every event's
// tokens (in and out) count against its LLM pool's token rate ceiling. Unlike
// the dollar record, tokens count even when the event carries no dollar
// amount. A nil ledger skips the token record.
func recordUsage(bl *autonomous.BudgetLedger, registry func() *inference.Registry, led *capacity.Ledger) func(session.UsageEvent) {
	return func(ev session.UsageEvent) {
		name := budgetLLMName(registry, ev.Backend)
		if led != nil {
			led.RecordTokens("llm:"+name, int64(ev.TokensIn)+int64(ev.TokensOut))
		}
		if bl == nil || ev.USD <= 0 {
			return
		}
		bl.RecordSession(name, ev.SessionID, ev.At, ev.USD, ev.TokensIn, ev.TokensOut)
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

// sessionLister is the part of the session manager hostExternalCount reads.
type sessionLister interface {
	Hostname() string
	ListSessions() []*session.Session
}

// hostExternalCount is the ledger's External callback: it counts operator and
// decompose sessions on this host against the "host" pool. It is called under
// the capacity ledger lock (rule on capacity.Options): Hostname is a field
// read and ListSessions copies the session store under its read lock, with no
// ledger call and no I/O.
func hostExternalCount(mgr sessionLister) func(pool string) int {
	return func(pool string) int {
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
}

// gpuNodeBackpressure is the ledger's Backpressure callback: a node: pool is
// blocked while its cached GPU utilisation is at or over the configured
// percentage. It is called under the capacity ledger lock (rule on
// capacity.Options): it reads the cached util map, which the slow tick
// refreshes.
func gpuNodeBackpressure(limitPct func() int, mu *sync.Mutex, util map[string]float64) func(pool string) (bool, string) {
	return func(pool string) (bool, string) {
		limit := limitPct()
		if limit <= 0 || !strings.HasPrefix(pool, "node:") {
			return false, ""
		}
		mu.Lock()
		u, ok := util[strings.TrimPrefix(pool, "node:")]
		mu.Unlock()
		if ok && u >= float64(limit) {
			return true, fmt.Sprintf("GPU %.0f%% busy (limit %d%%)", u, limit)
		}
		return false, ""
	}
}

// wireCapacity creates the admission ledger, connects it to the autonomous
// manager and the REST surface, and starts the sync loop that keeps pool
// limits current with config, compute nodes and LLMs, refreshes GPU
// backpressure, reaps leaked leases and raises long-wait alerts. It returns
// the ledger so the pin keepers can tell when a node is in use.
func wireCapacity(ctx context.Context, cfg *config.Config, mgr *session.Manager, amgr *autonomous.Manager, httpServer *server.HTTPServer, alertFn func(title, body string), nodeAlerts autonomous.NodeLoadAlerts) *capacity.Ledger {
	var gpuMu sync.Mutex
	gpuUtil := map[string]float64{}
	// servedModel is the size of the model each node last had loaded. syncPools
	// is the only writer, and it runs on one goroutine, so no lock is needed.
	servedModel := map[string]uint64{}
	servedCPUModel := map[string]uint64{}

	external := hostExternalCount(mgr)
	backpressure := gpuNodeBackpressure(func() int { return cfg.Autonomous.CapacityGPUUtilPct }, &gpuMu, gpuUtil)
	// A spawn refused by the BL30 global cooldown waits instead of failing;
	// the reset time words the task's wait reason.
	amgr.SetCooldownUntilFn(func() time.Time { return mgr.CooldownStatus().Until })
	registry := httpServer.InferenceRegistry
	autonomousBudget = newAutonomousBudget(cfg, expandHome(cfg.DataDir), registry)
	amgr.SetCostSources(autonomousBudget, sessionCostTotal(mgr.GetSession, registry))
	peerBudget := newPeerBudget(registry, httpServer.ServerStore)
	automationBudget := autonomous.NewAutomationBudget(amgr, autonomousBudget)
	automationBudget.Backoffs = autonomousBackoff.Live
	automationBudget.Peers = peerBudget
	automationBudget.Resets = automationBudget.ResetFor
	amgr.SetAutomationBudget(automationBudget)
	autonomousBudget.PeerSpend = peerBudget.Spend
	autonomousBudget.PeerBreakdown = peerBudget.Breakdown
	// Load the budget file now so Blocked never reads it under the ledger lock.
	autonomousBudget.Preload()
	nodeHealth := newNodeHealthView()
	led := capacity.New(capacity.Options{
		External:     external,
		Backpressure: backpressure,
		NodeHealthy:  nodeHealth.healthy,
		AutonomousBackpressure: composeBackpressure(
			backoffBackpressure(autonomousBackoff),
			budgetBackpressure(autonomousBudget),
		),
		HeldBackpressure: sharedNodeBackpressure(),
	})

	// Usage deltas feed both the dollar budget and the token rate ceilings.
	mgr.SetUsageEventHook(recordUsage(autonomousBudget, registry, led))

	resolve := capacityResolver(httpServer.InferenceRegistry, httpServer.ComputeRegistry)
	keys, choices := capacityManagerKeys(resolve)
	amgr.SetCapacity(led, keys)
	// An LLM entry edit (API, MCP, CLI or config reload) re-plans the waiters
	// at once; the pool sync ticker covers compute registry changes.
	replan := replanOnPlacementChange(led, httpServer.InferenceRegistry, httpServer.ComputeRegistry)
	if reg := httpServer.InferenceRegistry(); reg != nil {
		reg.SetChangeFn(replan)
	}
	amgr.SetCapacityChoices(choices)
	amgr.SetEntryNodes(entryNodeCount(httpServer.InferenceRegistry))
	amgr.SetSessionNode(sessionComputeNode(func(id string) (*session.Session, bool) { return mgr.GetSession(id) }, httpServer.ComputeRegistry))
	amgr.SetSessionEndpoint(sessionEndpoint(func(id string) (*session.Session, bool) { return mgr.GetSession(id) }))
	amgr.SetPiDefaultModel(piDefaultModel(cfg))
	amgr.SetNodeLoad(nodeLoader(httpServer.ComputeRegistry), nodeAlerts)
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
		st := led.SnapshotWatched(capacity.DefaultLockWatchdogThreshold, capacity.LogLockWedge)
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
	httpServer.SetCapacityAdmit(func(ctx context.Context, backend, model, holder string, priority int) (string, error) {
		pools, node, nodeChoices := resolve(backend, model)
		if len(pools) == 0 {
			return "", nil
		}
		req := withNodeChoices(interactiveAdmitRequest(holder, pools, node, model, priority), nodeChoices)
		req.Refresh = resolveRefresh(resolve, req, backend, model)
		if err := led.Acquire(ctx, req, 0, func() bool { return false }, func(string) {}); err != nil {
			return "", err
		}
		// The ledger chose one node for this holder among the candidates; report
		// it so the session starts on that node.
		if ls := led.LeaseOf(holder); ls != nil {
			return ls.Node, nil
		}
		return "", nil
	})
	httpServer.SetCapacityBind(func(holder, sessionID string) { led.Bind(holder, sessionID) })
	httpServer.SetCapacityRelease(func(holder string) { led.Release(holder) })

	// Guardrail judge: the same node:/llm: pools as the verifier, under its
	// own "judge:<unit id>" holders and its own registry instance (see
	// judgeAdmitters). Its one-shot session name binds to the lease the
	// same way the verifier's does.
	judges := newVerifierRegistry()
	_, _, judgeStartedFn := judgeAdmitters(led, judges, keys, amgr.CapacityWaitDuration())
	judgeCapacityAdmit = judgeNodeAdmitter(amgr, judges)
	// The judge's non-blocking probe for the verifier routing policy. It takes
	// the same judge lease as judgeCapacityAdmit with a short wait, then lets
	// go at once, so the attempt that follows admits for real.
	judgeCapacityTryAdmit = func(ctx context.Context, backend, model, unitID, prdID string) bool {
		_, release, err := amgr.AdmitJudge(ctx, autonomous.JudgeAdmit{
			Holder: autonomous.JudgeHolder(unitID), PRDID: prdID, Backend: backend, Model: model,
			Wait: 150 * time.Millisecond,
		})
		if err != nil {
			return false
		}
		release()
		return true
	}
	judgeSessionStarted = judgeStartedFn
	// Adopt, never double-spawn: on a daemon restart a guardrail-judge
	// session may still be live while the capacity ledger is empty, so the
	// one-shot runner consults the live-session listing and reuses an
	// already-running judge named exactly "autonomous:guardrail:<unit id>"
	// instead of spawning a second one. See judgeLiveSession in
	// session_judgment.go (it is consulted only for judge session names).
	judgeLiveSession = func(sessionName string) string {
		var bestID string
		var bestAt time.Time
		for _, s := range mgr.ListSessions() {
			if s.Name != sessionName || !sessionRunning(mgr, s.FullID) {
				continue
			}
			if bestID == "" || s.CreatedAt.After(bestAt) {
				bestID, bestAt = s.FullID, s.CreatedAt
			}
		}
		return bestID
	}
	// Judge leases ("judge:...") share the same live-session / spawn-grace
	// treatment as verifier leases under the reaper; the judges registry
	// also backs the startup guardrail-judge reconcile host.
	if amgr != nil {
		amgr.SetJudgeHost(&judgeReconcileHost{mgr: mgr, led: led, reg: judges})
	}

	verifiers := newVerifierRegistry()
	verifierSessionStarted = func(name, sessionID string) {
		if taskID, ok := verifierTaskFromSessionName(name); ok {
			holder := autonomous.VerifyHolder(taskID)
			verifiers.bindSession(holder, sessionID)
			led.Bind(holder, sessionID)
		}
	}

	// Read-only slot-state view for the one-shot verifier wait loop: it
	// reports whether the verifier's holder holds a lease, is queued, or
	// is off the ledger; it never acquires or releases (admission
	// happens daemon-side before the session spawns).
	verifierCapacitySlotState = func(holder string) autonomous.SlotState {
		if holder == "" {
			return autonomous.SlotNone
		}
		if led.Held(holder) {
			return autonomous.SlotHeld
		}
		for _, w := range led.Snapshot().Waiting {
			if w.Holder == holder {
				return autonomous.SlotWaiting
			}
		}
		return autonomous.SlotNone
	}

	amgr.SetVerifierHost(&verifierReconcileHost{mgr: mgr, led: led, reg: verifiers})

	wireVerifierAdmission(amgr, func(id string) int { return sessionPriority(mgr, id) }, verifiers)

	// councilCapacityAdmit (v8.38.0, BL390) — same shape and wait-duration
	// source as verifierCapacityAdmit; council runs are interactive/short
	// like the verifier's own /api/ask call, so reusing
	// autonomous.capacity_wait_seconds avoids a third copy of the same
	// config knob rather than inventing a council-specific one.
	councilCapacityAdmit = func(ctx context.Context, backend, model, holder, prdID string) (func(), error) {
		pools, node, nodeChoices := resolve(backend, model)
		if len(pools) == 0 {
			return func() {}, nil
		}
		waitDur := amgr.CapacityWaitDuration()
		req := withNodeChoices(councilAdmitRequest(holder, prdID, pools, node, model, sessionPriority(mgr, holder)), nodeChoices)
		req.Refresh = resolveRefresh(resolve, req, backend, model)
		if err := led.Acquire(ctx, req, waitDur, func() bool { return false }, func(string) {}); err != nil {
			return func() {}, err
		}
		return func() { led.Release(holder) }, nil
	}

	// verifierBackendLoad feeds the verifier policy's balance mode: the
	// highest used-over-limit among the backend's own pools. The shared
	// host pool is left out, since every backend draws on it and it would
	// make all of them read alike.
	verifierBackendLoad = func(backend, model string) float64 {
		pools, _ := keys(backend, model)
		own := map[string]bool{}
		for _, p := range pools {
			if p != "host" {
				own[p] = true
			}
		}
		var worst float64
		for _, ps := range led.Snapshot().Pools {
			if !own[ps.Name] || ps.Limit <= 0 {
				continue
			}
			if l := float64(ps.Held+ps.External) / float64(ps.Limit); l > worst {
				worst = l
			}
		}
		return worst
	}

	syncPools := func() {
		max := cfg.Session.MaxSessions
		if max > 0 {
			led.SetLimit("host", max-cfg.Session.EffectiveReservedInteractive())
		} else {
			led.SetLimit("host", 0)
		}
		// A compute node that hosts local engines takes its pool limit from its
		// slot count (compute.NodeSlots): the operator's session_slots wins;
		// otherwise one slot per GPU from observer data, limited by free GPU
		// memory, falling back to autonomous.max_engines_per_node when the
		// observer reports no GPU data. A node that hosts no local engine keeps
		// its max_concurrent_sessions limit, 0 meaning unlimited. Observer
		// figures are read only when a derived count or the GPU backpressure
		// gate needs them, in parallel so one slow monitoring endpoint does not
		// hold up the rest.
		engines := cfg.Autonomous.MaxEnginesPerNode
		if engines < 1 {
			engines = 1
		}
		if reg := httpServer.ComputeRegistry(); reg != nil {
			nodes := reg.List()
			nodeHealth.refresh(nodes)
			hosting := map[string]bool{}
			for _, entryNodes := range engineNodes(httpServer.InferenceRegistry()) {
				for _, node := range entryNodes {
					hosting[node] = true
				}
			}
			held := map[string]int{}
			for _, p := range led.Snapshot().Pools {
				held[p.Name] = p.Held
			}
			needsObserver := func(n *compute.Node) bool {
				return cfg.Autonomous.CapacityGPUUtilPct > 0 || (hosting[n.Name] && n.SessionSlots == 0)
			}
			observed := make([]compute.NodeObservation, len(nodes))
			var wg sync.WaitGroup
			for i, n := range nodes {
				if !needsObserver(n) {
					continue
				}
				wg.Add(1)
				go func(i int, name string) {
					defer wg.Done()
					observed[i], _ = httpServer.NodeObservation(name)
				}(i, n.Name)
			}
			wg.Wait()
			for i, n := range nodes {
				obs := observed[i]
				limit := n.MaxConcurrentSessions
				if hosting[n.Name] {
					// A model unloads when idle, so the size it last served is kept for
					// the derived count while none is loaded.
					var loaded uint64
					for _, b := range obs.LoadedModelBytes {
						if b > loaded {
							loaded = b
						}
					}
					if loaded > 0 {
						servedModel[n.Name] = loaded
					}
					obs.LastModelBytes = servedModel[n.Name]
					// The CPU path keeps its own last size, the same way.
					var cpuLoaded uint64
					for _, b := range obs.CPUModelBytes {
						if b > cpuLoaded {
							cpuLoaded = b
						}
					}
					if cpuLoaded > 0 {
						servedCPUModel[n.Name] = cpuLoaded
					}
					obs.LastCPUModelBytes = servedCPUModel[n.Name]
					limit = compute.NodeSlots(n.SessionSlots, n.MaxConcurrentSessions, engines, held["node:"+n.Name], obs)
				}
				led.SetLimit("node:"+n.Name, limit)
				if cfg.Autonomous.CapacityGPUUtilPct > 0 && len(obs.GPUs) > 0 {
					gpuMu.Lock()
					gpuUtil[n.Name] = obs.MaxUtilPct
					gpuMu.Unlock()
				}
			}
		}
		if reg := httpServer.InferenceRegistry(); reg != nil {
			syncLLMLimits(cfg, reg, led)
			syncLLMRates(cfg, reg, led)
		}
		// The reaper runs live once per lease, in order, on this goroutine, so the
		// verdicts map needs no lock. Each released lease gets one journal line.
		sessionOf := map[string]string{}
		whys := map[string]string{}
		live := func(ls capacity.Lease) bool {
			ok, why := leaseLiveVerdict(ls, verifiers, judges.isActive, func(id string) bool { return sessionRunning(mgr, id) }, time.Now())
			if !ok {
				sid := ls.SessionID
				if verifiers != nil && verifiers.sessionOf(ls.Holder) != "" {
					sid = verifiers.sessionOf(ls.Holder)
				}
				sessionOf[ls.Holder] = sid
				whys[ls.Holder] = why
			}
			return ok
		}
		for _, h := range led.Reap(live) {
			fmt.Printf("[capacity] reaped leaked lease holder=%s session=%s: %s\n", h, sessionOf[h], whys[h])
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
				replan()
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

// wireVerifierAdmission sets the verifier's capacity hooks over amgr's ledger:
// the blocking admission, its non-blocking sibling, the dry run and the change
// channel. priority maps a session id to its capacity priority; verifiers
// tracks the running verifier leases.
func wireVerifierAdmission(amgr *autonomous.Manager, priority func(sessionID string) int, verifiers *verifierRegistry) {
	// The verifier takes its lease through AdmitJudge. Its hand-off (the
	// worker's lease re-keyed to verify:<task> so the task keeps one slot from
	// spawn through verification, on the same node) is Manager.HandoffVerifyLease's
	// logic (judgeHandoff): the worker's lease is
	// handed over when the verifier can run on it (keeping the worker's node),
	// otherwise the worker's slot is released first and the verifier acquires
	// its own. The node is the one the verifier holds, "" for a one-node entry.
	verifierCapacityAdmit = func(ctx context.Context, backend, model, taskID, prdID, sessionID string) (func(), string, error) {
		holder := autonomous.VerifyHolder(taskID)
		node, release, err := amgr.AdmitJudge(ctx, autonomous.JudgeAdmit{
			Holder: holder, WorkerHolder: taskID, PRDID: prdID, Backend: backend, Model: model,
			Priority: priority(sessionID), Wait: amgr.CapacityWaitDuration(),
		})
		if err != nil {
			return func() {}, "", err
		}
		end := verifiers.begin(holder)
		return func() { end(); release() }, node, nil
	}

	// verifierCapacityTryAdmit (v8.36.9) is verifierCapacityAdmit's
	// non-blocking sibling — used to probe a candidate in
	// autonomous.verification_backends without committing to its
	// normal (potentially hours-long) wait. A short, real Acquire call
	// rather than a Snapshot-then-Acquire check: Acquire's own fast
	// path already grants immediately when a pool has room, so this
	// just bounds how long a genuinely-busy candidate is given before
	// the caller moves on to the next one.
	// The probe runs AdmitJudge with a short wait: the worker's lease is
	// handed over or released first (idempotent), so a candidate either takes
	// its own slot or does not, never a second slot on top of the worker's.
	verifierCapacityTryAdmit = func(ctx context.Context, backend, model, taskID, prdID, sessionID string) (func(), string, bool) {
		holder := autonomous.VerifyHolder(taskID)
		node, release, err := amgr.AdmitJudge(ctx, autonomous.JudgeAdmit{
			Holder: holder, WorkerHolder: taskID, PRDID: prdID, Backend: backend, Model: model,
			Priority: priority(sessionID), Wait: 150 * time.Millisecond,
		})
		if err != nil {
			return func() {}, "", false
		}
		end := verifiers.begin(holder)
		return func() { end(); release() }, node, true
	}

	// verifierCapacityFits is the dry run of the same admission, for a verifier
	// that waits and re-runs its policy: the worker lease is already released or
	// handed over by then, so WorkerHolder is empty and only the verifier's own
	// pools count. The priority is the task session's, as in the probe above.
	verifierCapacityFits = func(backend, model, taskID, prdID string) bool {
		return amgr.JudgeFits(autonomous.JudgeAdmit{
			Holder: autonomous.VerifyHolder(taskID), PRDID: prdID, Backend: backend, Model: model,
			Priority: priority(verifierTaskSession(amgr, taskID)),
		})
	}
	verifierCapacityChanged = amgr.CapacityChanged
}

// verifierTaskSession returns the session the task's worker ran in, "" when the
// task is unknown. The dry run in verifierCapacityFits has no session id of its
// own, and the priority belongs to that session.
func verifierTaskSession(amgr *autonomous.Manager, taskID string) string {
	if amgr == nil {
		return ""
	}
	st := amgr.Store()
	t, ok := st.GetTask(taskID)
	if !ok {
		return ""
	}
	var id string
	st.WithLock(func() { id = t.SessionID })
	return id
}

// sessionPriority returns the capacity priority of the session id, or 0 when
// there is no such session (a council run or an unspawned task has none).
func sessionPriority(mgr *session.Manager, id string) int {
	if mgr == nil || id == "" {
		return 0
	}
	if sess, ok := mgr.GetSession(id); ok && sess != nil {
		return sess.Priority
	}
	return 0
}

func interactiveAdmitRequest(holder string, pools []string, node, model string, priority int) capacity.Request {
	return capacity.Request{Holder: holder, Pools: pools, Node: node, Model: model, Interactive: true, Priority: priority}
}

func councilAdmitRequest(holder, prdID string, pools []string, node, model string, priority int) capacity.Request {
	return capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, Model: model, Priority: priority}
}

// sessionComputeNode builds the resolver that names the compute node a session
// runs on: its explicit ComputeNodeRef first, and only when that is empty the
// node whose address matches the host the session was placed on. It returns ""
// for a session that is unknown or runs on no registered node.
func sessionComputeNode(lookup func(id string) (*session.Session, bool), nodes func() *compute.Registry) autonomous.SessionNodeFn {
	return func(sessionID string) string {
		if lookup == nil {
			return ""
		}
		s, ok := lookup(sessionID)
		if !ok || s == nil {
			return ""
		}
		if s.ComputeNodeRef != "" {
			return s.ComputeNodeRef
		}
		var reg *compute.Registry
		if nodes != nil {
			reg = nodes()
		}
		if reg == nil {
			return ""
		}
		for _, addr := range []string{s.NodeAddress, s.OllamaURL} {
			if want := addressHost(addr); want != "" {
				for _, n := range reg.List() {
					if addressHost(n.Address) == want {
						return n.Name
					}
				}
			}
		}
		return ""
	}
}

// sessionEndpoint builds the reader of the endpoint a session was placed on:
// its node address, else its Ollama URL ("" when it records neither).
func sessionEndpoint(lookup func(id string) (*session.Session, bool)) autonomous.SessionEndpointFn {
	return func(sessionID string) string {
		if lookup == nil {
			return ""
		}
		s, ok := lookup(sessionID)
		if !ok || s == nil {
			return ""
		}
		if s.NodeAddress != "" {
			return s.NodeAddress
		}
		return s.OllamaURL
	}
}

// addressHost reduces an address (with or without scheme and path) to its
// lowercase host:port so a session's recorded endpoint and a node's address
// compare equal however each was written.
func addressHost(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return ""
	}
	if _, rest, ok := strings.Cut(addr, "://"); ok {
		addr = rest
	}
	host, _, _ := strings.Cut(addr, "/")
	return host
}
