package autonomous

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// JudgeAdmit describes the capacity admission of one judge: the verifier's
// own call, or a guardrail's judgment. The judge takes a lease like a worker
// does. When its LLM entry pools two or more nodes, the ledger places it on
// one of them and the model is made resident there before the judge starts.
type JudgeAdmit struct {
	// Holder is the judge's own lease name (VerifyHolder for the verifier).
	Holder string
	// WorkerHolder is the task's worker lease to hand over, or "" for none.
	// On a node with a verifier pool (see verifierPoolOnWorkerNode) it is
	// released instead, so the verifier takes node-verify:<node>.
	WorkerHolder string
	PRDID        string
	Backend      string
	Model        string
	Priority     int
	// Wait bounds a fresh acquire; zero waits without bound.
	Wait time.Duration
	// Cancelled ends a queued acquire early when it reports true.
	Cancelled func() bool
	// NodeOnly takes no lease at all when the entry has fewer than two nodes,
	// so such a judge starts exactly as it did before judges held leases.
	NodeOnly bool

	// preferNode is the node of a lease the judge held on entry (set by
	// AdmitJudge): the tie breaker when placement finds equally loaded nodes.
	preferNode string

	// followNode is the worker's node a verifier must be placed on, and
	// followExclude the other candidates kept out for it (set by AdmitJudge and
	// JudgeFits when verifier_follows_worker_node applies; see verifier_node.go).
	followNode    string
	followExclude []string
}

// AdmitJudge takes the capacity lease a judge runs under and returns the node
// the judge holds, with the release func that frees the lease.
//
// The worker's lease is handed over when the judge can run on it (see
// judgeHandoff). On a node that has a verifier pool the hand-over is skipped
// and the worker's slot is released instead: the ledger places the judge on
// node-verify:<node> so it runs while executor may still hold the node's
// executor pool. Otherwise the worker's slot is released first (or handed
// over) and the judge acquires its own lease on its pools, with its candidate
// nodes so the ledger picks one. The node is returned only for an entry with
// two or more candidates; a single-node judge gets "" and starts as it always
// has.
//
// For a multi-node entry the model is loaded on the node the lease landed on.
// A load that fails raises the node-load alert, releases the lease and acquires
// again without that node, until no candidate is left and the load error is
// returned.
func (m *Manager) AdmitJudge(ctx context.Context, j JudgeAdmit) (string, func(), error) {
	noop := func() {}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	if led == nil || keysFn == nil {
		return "", noop, nil
	}
	choices := nodeChoicesFor(choicesFn, j.Backend, j.Model)
	if j.NodeOnly && len(choices) < 2 {
		return "", noop, nil
	}
	// A lease the judge still holds on entry belongs to an earlier run of it that
	// never released. Acquire returns at once for a holder that is already
	// admitted, so the respawned judge would keep that node without placement.
	// Free it and ask placement to choose again, with the old node as the tie
	// breaker.
	if ls := led.LeaseOf(j.Holder); ls != nil {
		j.preferNode = ls.Node
		led.Release(j.Holder)
	}
	// A verifier admitted for a worker follows the node that worker ran on; its
	// lease is read before it is handed over or released below. The verifier's
	// own earlier lease is not the worker's node: it is the stale lease just
	// released. An admission that names no worker is placed again like a new
	// request.
	if j.WorkerHolder != "" {
		leaseNode := ""
		if ls := led.LeaseOf(j.WorkerHolder); ls != nil {
			leaseNode = ls.Node
		}
		m.followWorkerNode(ctx, &j, choices, leaseNode, true)
	}
	if j.WorkerHolder != "" {
		if !verifierPoolOnWorkerNode(led, j.WorkerHolder) {
			if _, ok := judgeHandoff(led, keysFn, choicesFn, j.WorkerHolder, j.Holder, j.Backend, j.Model); ok {
				return m.placeJudge(ctx, j, led, choices)
			}
		}
		led.Release(j.WorkerHolder)
	}
	pools, node := keysFn(j.Backend, j.Model)
	if len(pools) == 0 {
		return "", noop, nil
	}
	if err := led.Acquire(ctx, m.judgeRequestRefreshed(j, pools, node, choices, j.followExclude), j.Wait, judgeCancelled(j), func(string) {}); err != nil {
		return "", noop, err
	}
	return m.placeJudge(ctx, j, led, choices)
}

// CapacityChanged returns the channel the capacity ledger closes the next time
// capacity changes (see capacity.Ledger.Changed), so a caller can wait for room
// instead of polling. With no ledger wired it is a nil channel, which blocks
// forever, so a select on it is always safe. Take the channel before checking
// JudgeFits and select on it afterwards, so a change in between is not lost.
func (m *Manager) CapacityChanged() <-chan struct{} {
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	if led == nil {
		return nil
	}
	return led.Changed()
}

// JudgeFits is a dry run of AdmitJudge for the same JudgeAdmit: it reports
// whether the judge's lease would be granted right now. It resolves pools, node
// and candidate nodes as AdmitJudge does, but never queues, takes a lease,
// hands over or releases a worker lease, or loads a model on a node. WorkerHolder
// is ignored: by the time a verifier waits, the worker lease is already released
// or handed over. Whatever AdmitJudge admits at once (no ledger, no resolver, no
// pools, or a NodeOnly judge on a single-node entry) fits.
func (m *Manager) JudgeFits(j JudgeAdmit) bool {
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	if led == nil || keysFn == nil {
		return true
	}
	choices := nodeChoicesFor(choicesFn, j.Backend, j.Model)
	if j.NodeOnly && len(choices) < 2 {
		return true
	}
	pools, node := keysFn(j.Backend, j.Model)
	if len(pools) == 0 {
		return true
	}
	// The worker's lease is gone by the time a verifier waits, so its node comes
	// from the pin or the node recorded on the task. The verifier's real
	// admission always names its worker, so the dry run follows as it does.
	m.followWorkerNode(context.Background(), &j, choices, "", false)
	return led.WouldGrant(m.judgeRequestRefreshed(j, pools, node, choices, j.followExclude))
}

// placeJudge finishes an admission: it reads the node the judge's lease holds,
// makes the model resident there when the entry pools two or more nodes, and
// fails over to another candidate when the load fails.
func (m *Manager) placeJudge(ctx context.Context, j JudgeAdmit, led *capacity.Ledger, choices []capacity.NodeChoice) (string, func(), error) {
	noop := func() {}
	release := func() { led.Release(j.Holder) }
	if len(choices) < 2 {
		return "", release, nil
	}
	m.mu.Lock()
	loader, sink := m.nodeLoad, m.nodeAlerts
	m.mu.Unlock()
	node := ""
	if ls := led.LeaseOf(j.Holder); ls != nil {
		node = ls.Node
	}
	// A verifier that follows its worker's node starts with every other
	// candidate excluded, so a node that fails to load leaves none to move to.
	excluded := slices.Clone(j.followExclude)
	for loader != nil && node != "" {
		lerr := loader(ctx, node, j.Model, func(string) {})
		if lerr == nil {
			break
		}
		if ctx.Err() != nil {
			led.Release(j.Holder)
			return "", noop, ctx.Err()
		}
		raiseNodeLoadAlert(sink, node, j.Model, lerr)
		led.Release(j.Holder)
		excluded = append(excluded, node)
		if !nodeLeft(choices, excluded) {
			return "", noop, fmt.Errorf("node load: %w", lerr)
		}
		pools, _ := m.judgeKeys(j)
		if err := led.Acquire(ctx, m.judgeRequestRefreshed(j, pools, "", choices, excluded), j.Wait, judgeCancelled(j), func(string) {}); err != nil {
			return "", noop, err
		}
		node = ""
		if ls := led.LeaseOf(j.Holder); ls != nil {
			node = ls.Node
		}
	}
	return node, release, nil
}

// verifierPoolOnWorkerNode reports whether the node the worker lease holds
// has a verifier pool — the ledger admits a limit of verifier sessions on
// node-verify:<node> there (Ledger.VerifierSlotsOn above 0). It is the
// AdmitJudge condition for skipping the worker-lease hand-over: a judge that
// can run on the verifier pool does not take a slot out of the executor pool
// node:<node>, so the worker's executor lease is released (and the judge's own
// lease lands on node-verify:<node> through the ledger as usual) instead of
// being handed over.
//
// No I/O under a lock: VerifierSlotsOn is the ledger's read of a map under its
// own lock, and LeaseOf is the same — nothing here calls another ledger method
// with side effects, so the LOCK RULE holds.
func verifierPoolOnWorkerNode(led *capacity.Ledger, workerHolder string) bool {
	ls := led.LeaseOf(workerHolder)
	if ls == nil || ls.Node == "" {
		return false
	}
	return led.VerifierSlotsOn(ls.Node) > 0
}

// judgeKeys resolves the judge's pools through the wired resolver.
func (m *Manager) judgeKeys(j JudgeAdmit) ([]string, string) {
	m.mu.Lock()
	keysFn := m.capacityKeys
	m.mu.Unlock()
	if keysFn == nil {
		return nil, ""
	}
	return keysFn(j.Backend, j.Model)
}

// judgeRequest builds the ledger request for a judge's own lease. Candidate
// nodes go on the request only for an entry with two or more of them.
func judgeRequest(j JudgeAdmit, pools []string, node string, choices []capacity.NodeChoice, exclude []string) capacity.Request {
	req := capacity.Request{
		Holder: j.Holder, PRDID: j.PRDID, Priority: j.Priority, Pools: pools, Node: node,
		Model: j.Model, Role: capacity.RoleVerifier, ExcludeNodes: exclude, PreferNode: j.preferNode,
	}
	if len(choices) >= 2 {
		req.NodeChoices = choices
	}
	return req
}

// judgeRequestRefreshed is judgeRequest with Refresh set, so a node the compute
// registry learns while the judge waits joins its candidates. The exclude list
// carries over.
func (m *Manager) judgeRequestRefreshed(j JudgeAdmit, pools []string, node string, choices []capacity.NodeChoice, exclude []string) capacity.Request {
	req := judgeRequest(j, pools, node, choices, exclude)
	// A verifier that follows its worker's node keeps the exclusion while it
	// waits: every refresh excludes all candidates but that node, including a
	// node that joins the candidates meanwhile, and a refresh that finds the
	// node gone leaves the request unplaceable instead of moving it.
	req.Refresh = m.placementRefreshPinned(req, j.Backend, j.Model, nil, j.followNode, exclude)
	return req
}

// judgeCancelled returns the judge's cancel check, or one that never cancels.
func judgeCancelled(j JudgeAdmit) func() bool {
	if j.Cancelled != nil {
		return j.Cancelled
	}
	return func() bool { return false }
}
