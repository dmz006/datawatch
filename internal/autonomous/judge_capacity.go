package autonomous

import (
	"context"
	"fmt"
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
}

// AdmitJudge takes the capacity lease a judge runs under and returns the node
// the judge holds, with the release func that frees the lease.
//
// The worker's lease is handed over when the judge can run on it (see
// judgeHandoff). Otherwise the worker's slot is released first and the judge
// acquires its own lease on its pools, with its candidate nodes so the ledger
// picks one. The node is returned only for an entry with two or more
// candidates; a single-node judge gets "" and starts as it always has.
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
	if j.WorkerHolder != "" {
		if _, ok := judgeHandoff(led, keysFn, choicesFn, j.WorkerHolder, j.Holder, j.Backend, j.Model); ok {
			return m.placeJudge(ctx, j, led, choices)
		}
		led.Release(j.WorkerHolder)
	}
	pools, node := keysFn(j.Backend, j.Model)
	if len(pools) == 0 {
		return "", noop, nil
	}
	if err := led.Acquire(ctx, m.judgeRequestRefreshed(j, pools, node, choices, nil), j.Wait, judgeCancelled(j), func(string) {}); err != nil {
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
	return led.WouldGrant(m.judgeRequestRefreshed(j, pools, node, choices, nil))
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
	var excluded []string
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
		Model: j.Model, Role: capacity.RoleVerifier, ExcludeNodes: exclude,
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
	req.Refresh = m.placementRefresh(req, j.Backend, j.Model, nil)
	return req
}

// judgeCancelled returns the judge's cancel check, or one that never cancels.
func judgeCancelled(j JudgeAdmit) func() bool {
	if j.Cancelled != nil {
		return j.Cancelled
	}
	return func() bool { return false }
}
