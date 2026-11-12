// Planning requeue (shared helper).
//
// A PRD's planning run (the decompose it launches from Decompose /
// DecomposeStreaming) resolves its pool from the PRD's current
// decomposition_profile / decomposition_model at admission time
// (decomposeAdmitted). When the operator changes one of those fields while
// the planning run is still sitting in the capacity queue, the queued
// request must move to the pool the CURRENT profile resolves to, and the
// run must then execute with the current pair. This file is the shared
// helper for that move (reusable by the worker story the same way
// requeueQueuedCapacity is, see queued_task_override.go):
//
//   - Queued (not started) -> its single queue entry is retargeted in place
//     to the new pool (capacity.Ledger.Retarget): the old queue slot is
//     cancelled and the run is re-queued on the new pool — one live request
//     before, one live request after, on the latest pool. Re-targeting an
//     entry that already carries the desired placement is a no-op, so
//     changing the profile twice leaves exactly one queued request and no
//     leaked capacity.
//   - Started (holds a live lease) -> left entirely alone; the new profile
//     applies from the NEXT planning run, and the result carries a plain
//     English reason (including "wait for it to finish or cancel it").
//   - No live planning run -> the value is stored and applies at the next
//     decompose.
//
// The surfaced wait reason on the PRD always names the pool the run is
// CURRENTLY waiting for: decomposeAdmitted names the pool at admission, and
// every requeue re-surfaces the reason with the new pool named.
//
// The structured result travels as PlanningRequeueResult through the REST,
// MCP and CLI surfaces (SetLLMOut.Planning on the set_llm path, the
// decompose handler's in-progress response).

package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// PlanningOutcome is the structured outcome of a planning requeue
// (requirement: outcome = requeued | deferred_to_next_run | unchanged).
type PlanningOutcome string

const (
	// PlanningRequeued: the queued planning run was retargeted to the pool
	// the current profile resolves to.
	PlanningRequeued PlanningOutcome = "requeued"
	// PlanningDeferred: the planning run already started and holds a live
	// lease; it was left alone and the change applies from the next run.
	PlanningDeferred PlanningOutcome = "deferred_to_next_run"
	// PlanningUnchanged: nothing to move — no live planning run, or the
	// queue entry already sits on the target pool.
	PlanningUnchanged PlanningOutcome = "unchanged"
)

// PlanningRequeueResult reports old and new backend/model/pool plus a plain
// English reason whenever the answer is not a plain requeue.
type PlanningRequeueResult struct {
	Outcome    PlanningOutcome `json:"outcome"`
	OldBackend string          `json:"old_backend,omitempty"`
	NewBackend string          `json:"new_backend,omitempty"`
	OldModel   string          `json:"old_model,omitempty"`
	NewModel   string          `json:"new_model,omitempty"`
	OldPools   []string        `json:"old_pools,omitempty"`
	NewPools   []string        `json:"new_pools,omitempty"`
	Reason     string          `json:"reason,omitempty"`
}

// plannerHolder is the capacity holder key decomposeAdmitted queues the
// planner under (see capacity.go).
func plannerHolder(prdID string) string { return "plan:" + prdID }

// plannerPairFor mirrors the per-PRD resolution Decompose /
// DecomposeStreaming perform: the PRD's decomposition_profile (falling back
// to the effective planning backend) plus the same model fallback chain.
func (m *Manager) plannerPairFor(prd *PRD) (backend, model string) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	planBackend, planGlobalModel := effectivePlanning(m.AutomationForPRD(prd), cfg)
	backend = prd.DecompositionProfile
	if backend == "" {
		backend = planBackend
	}
	model = resolvePlannedModel(backend, prd.DecompositionModel, prd.Backend, prd.Model, planBackend, planGlobalModel, cfg.VerificationModel)
	return backend, model
}

// planningRequestAtGrant re-resolves the planner's backend/model from the
// CURRENT record for the decompose request that runs once the capacity
// admission clears.
//
// Decompose and decomposeStreamingCore resolve the pair once, before the
// wait, and hold it across the capacity wait inside
// decomposeAdmitted. If the operator changes the decomposition profile or
// its model while the planner is queued, requeuePlanningRun retargets the
// queue entry onto the pool the NEW profile resolves to — and the request
// follows with it (requirement a "run immediately on the new pool": the
// execution must carry the NEW pair, not the one resolved before the
// capacity wait). The admission was granted on the pool of the current
// target (Retarget carried it), so the executing pair is the one the
// capacity decision was made for.
//
// If the pair the record now resolves to is unresolvable (empty backend) or
// fails the same pin check an admission requires (an unpinned pi-family
// pair), the request keeps the pair it was handed: entry already validated
// it, and requeuePlanningRun refuses the retarget in that very case
// (keeping the entry on the old pool), so pool and pair stay consistent.
func (m *Manager) planningRequestAtGrant(prd *PRD, in DecomposeRequest) DecomposeRequest {
	backend, model := m.plannerPairFor(prd)
	if backend == "" {
		return in
	}
	if serr := RequirePinnedModel(backend, model,
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); serr != nil {
		return in
	}
	in.Backend = backend
	in.Model = model
	return in
}

// plannerPoolsFor is the pool a planning run queues on for a backend/model
// pair — the same resolution and "host" trim used at planner admission.
func (m *Manager) plannerPoolsFor(backend, model string) []string {
	m.mu.Lock()
	keysFn := m.capacityKeys
	m.mu.Unlock()
	if keysFn == nil {
		return nil
	}
	pools, _ := keysFn(backend, model)
	return decomposePools(pools)
}

// sortedCopyPools returns a sorted copy so the reported pool lists compare
// and render stably (order carries no meaning for a pool set).
func sortedCopyPools(pools []string) []string {
	out := append([]string(nil), pools...)
	sort.Strings(out)
	return out
}

// plannerWaiting reports the planner's live capacity wait for prdID: the
// pools its queue entry currently sits on and the reason the ledger last
// computed for it.
func plannerWaiting(led *capacity.Ledger, prdID string) (pools []string, reason string, ok bool) {
	w := plannerWaiter(led, prdID)
	if w == nil {
		return nil, "", false
	}
	return w.Pools, w.Reason, true
}

func plannerWaiter(led *capacity.Ledger, prdID string) *capacity.Waiter {
	if led == nil {
		return nil
	}
	holder := plannerHolder(prdID)
	snap := led.Snapshot()
	for i := range snap.Waiting {
		if snap.Waiting[i].Holder == holder {
			return &snap.Waiting[i]
		}
	}
	return nil
}

// requeuePlanningRun is the shared planning-requeue helper. oldBackend /
// oldModel is the pair the call is changing away from (for the reported
// old_pools); the target is always resolved from the PRD's CURRENT record,
// never a cached value. It moves the queued run in place (no double entry,
// no leaked capacity), leaves a started run alone with an explanation, and
// reports unchanged when there is nothing to move.
func (m *Manager) requeuePlanningRun(prdID, actor, oldBackend, oldModel string) *PlanningRequeueResult {
	res := &PlanningRequeueResult{OldBackend: oldBackend, OldModel: oldModel}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		res.Outcome = PlanningUnchanged
		res.Reason = "prd not found"
		return res
	}
	newBackend, newModel := m.plannerPairFor(prd)
	res.NewBackend, res.NewModel = newBackend, newModel
	res.NewPools = sortedCopyPools(m.plannerPoolsFor(newBackend, newModel))

	led := m.Capacity()
	if led == nil {
		res.Outcome = PlanningUnchanged
		res.Reason = "no planning run is queued or started, and capacity accounting is off; the stored value applies at the next decompose"
		return res
	}

	// Old pools: as actually queued (the queue entry wins — it may have been
	// retargeted by an earlier change), else as the old pair resolves.
	if pools, _, ok := plannerWaiting(led, prdID); ok {
		res.OldPools = sortedCopyPools(pools)
	} else if len(res.OldPools) == 0 {
		res.OldPools = sortedCopyPools(m.plannerPoolsFor(oldBackend, oldModel))
	}

	// A STARTED planning run (holding a live lease) is always left alone,
	// whatever the pool math says: the change applies from the next run.
	if led.Held(plannerHolder(prdID)) {
		res.Outcome = PlanningDeferred
		res.Reason = fmt.Sprintf(
			"the planning run has already started (it holds a lease on %s); it was left alone and the new profile applies from the next decompose — wait for the running decompose to finish (or cancel it) to requeue on %s",
			strings.Join(res.OldPools, ", "), strings.Join(res.NewPools, ", "))
		m.notePlanningRequeue(prdID, actor, res)
		return res
	}

	// Queued, but already on the target pool: nothing to move.
	if poolsEqual(res.OldPools, res.NewPools) {
		if plannerWaiter(led, prdID) != nil {
			res.Outcome = PlanningUnchanged
			res.Reason = fmt.Sprintf("the planning run is already queued on %s; nothing to move", strings.Join(res.NewPools, ", "))
		} else {
			res.Outcome = PlanningUnchanged
			res.Reason = "no planning run is queued or started; the stored value applies at the next decompose"
		}
		return res
	}

	// The new pair must be pinnable the same way a decompose admission
	// requires; a pair that could not even be admitted is not retargetable.
	if err := RequirePinnedModel(newBackend, newModel,
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); err != nil {
		res.Outcome = PlanningUnchanged
		res.Reason = "not retargeted: " + err.Error() + "; the queued run keeps its pool and the stored value applies at the next decompose"
		return res
	}

	// Build the request the same way decomposeAdmitted builds its admission
	// request, so a retarget onto a pool with free capacity grants
	// immediately (no further ledger poll).
	m.mu.Lock()
	keysFn, choicesFn := m.capacityKeys, m.capacityChoices
	priority := prd.Priority
	m.mu.Unlock()
	var (
		pools   = res.NewPools
		node    string
		choices []capacity.NodeChoice
	)
	if keysFn != nil {
		all, n := keysFn(newBackend, newModel)
		pools = decomposePools(all)
		node = n
	}
	choices = nodeChoicesFor(choicesFn, newBackend, newModel)
	req := capacity.Request{
		Holder: plannerHolder(prdID), PRDID: prdID, Priority: priority,
		Pools: pools, Node: node, NodeChoices: choices,
		Model: newModel, Role: capacity.RolePlanner, Consumer: consumerSessionSpawn,
	}
	req.Refresh = m.placementRefresh(req, newBackend, newModel, decomposePools)

	if led.Retarget(plannerHolder(prdID), req) {
		res.Outcome = PlanningRequeued
		res.Reason = fmt.Sprintf("the queued planning run was retargeted from %s to %s", strings.Join(res.OldPools, ", "), strings.Join(res.NewPools, ", "))
		if _, reason, queued := plannerWaiting(led, prdID); queued {
			// Keep the surfaced reason on the CURRENT pool: name the pool the
			// run is waiting for now (requirement c).
			m.setPRDWaitReason(prdID, m.plannerWaitReason(newBackend, newModel, reason))
		}
		m.notePlanningRequeue(prdID, actor, res)
		return res
	}

	res.Outcome = PlanningUnchanged
	res.Reason = "no planning run is queued or started; the stored value applies at the next decompose"
	return res
}

// poolsEqual compares two pool sets order-insensitively (sortedCopyPools
// input assumed).
func poolsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// plannerWaitReason is the wait reason a planner queues with: the ledger's
// reason with the pool(s) it is waiting for named (requirement c — the wait
// reason always names the pool from the CURRENT profile).
func (m *Manager) plannerWaitReason(backend, model, reason string) string {
	return nameRequestedPool(reason, m.plannerPoolsFor(backend, model))
}

// RestartPlanning handles a decompose (re)requested while the PRD is
// already planning (requirement d): it re-queues the queued planning run on
// the CURRENT pool — instead of double-spawning — or, when the run already
// started, returns the explanation (wait for the running decompose to
// finish, or cancel it). The PRD must exist and be planning; the callers
// (the decompose handlers) enforce that.
func (m *Manager) RestartPlanning(prdID string) *PlanningRequeueResult {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		res := &PlanningRequeueResult{Outcome: PlanningUnchanged}
		res.Reason = "prd not found"
		return res
	}
	// The change is against the pair the run was queued with: for the
	// "old" side, the pair the CURRENT record still resolves to (a restart
	// does not mutate the record) — the queue entry itself wins for the
	// reported old_pools.
	oldBackend, oldModel := m.plannerPairFor(prd)
	res := m.requeuePlanningRun(prdID, "restart", oldBackend, oldModel)
	if res.Outcome == PlanningDeferred {
		// Requirement d: the explanation must say the decompose is already
		// running — wait for it to finish or cancel it.
		res.Reason = "decompose is already running (the planning run holds a live lease): wait for it to finish, or cancel the PRD to requeue on the current pool. " + res.Reason
	}
	return res
}

// notePlanningRequeue records the requeue decision on the PRD (audit) and
// emits its update so the surfaces pick it up.
func (m *Manager) notePlanningRequeue(prdID, actor string, res *PlanningRequeueResult) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "planning_requeue", Actor: actor,
		Note: fmt.Sprintf("outcome=%s old=%s/%s pools=%s new=%s/%s pools=%s reason=%s",
			res.Outcome, res.OldBackend, res.OldModel, strings.Join(res.OldPools, ","),
			res.NewBackend, res.NewModel, strings.Join(res.NewPools, ","), res.Reason),
	})
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, prd.Status, nil, nil)
	}
}
