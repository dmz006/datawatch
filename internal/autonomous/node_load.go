package autonomous

import (
	"context"
	"fmt"
	"slices"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
)

// NodeLoader makes model resident on the named compute node before a worker
// spawns there. wait is called with a reason text while the node loads the
// model; the task shows it as its waiting state. A returned error is the
// cause that fails the node for this attempt.
type NodeLoader func(ctx context.Context, node, model string, wait func(reason string)) error

// NodeLoadAlerts receives the error alert raised when a node fails to load a
// model. *alerts.Store satisfies it.
type NodeLoadAlerts interface {
	Add(level alerts.Level, title, body, sessionID string) *alerts.Alert
}

// loadWorkerNode runs right after a worker lease is granted. With a placement
// advisor wired, the gate already ran in admitPreferring and the model is
// resident on the leased node: this is a no-op. Without an advisor (tests,
// capacity off), it is the safety net that makes the model resident on the
// node the lease landed on, with the same failover exclusion as before: a
// node that fails is alerted, the lease is released, and the lease is
// acquired again with that node excluded.
//
// A single-node entry has no load step: it returns nil without touching the
// loader.
func (m *Manager) loadWorkerNode(ctx context.Context, prd *PRD, t *Task, backend, model string) error {
	// With a placement advisor the gate ran in admitPreferring and loaded the
	// model on the target node; the lease is already on that node. Nothing to
	// do here.
	if m.getPlacementAdvisor() != nil {
		return nil
	}
	m.mu.Lock()
	loader, sink := m.nodeLoad, m.nodeAlerts
	m.mu.Unlock()
	choices := nodeChoicesFor(m.CapacityChoices(), backend, model)
	if loader == nil || len(choices) < 2 {
		return nil
	}
	var excluded []string
	for {
		node := m.heldNode(t.ID)
		if node == "" {
			return nil
		}
		lerr := m.loadOnNode(ctx, t, loader, node, model)
		if lerr == nil {
			return nil
		}
		if ctx.Err() != nil {
			if m.taskCancelled(t.ID) {
				return errCancelledWhileWaiting
			}
			return ctx.Err()
		}
		raiseNodeLoadAlert(sink, node, model, lerr)
		m.releaseCapacity(t.ID)
		excluded = append(excluded, node)
		if !nodeLeft(choices, excluded) {
			return fmt.Errorf("node load: %w", lerr)
		}
		if aerr := m.acquireLease(ctx, prd, t, backend, model, capacity.RoleWorker, excluded, ""); aerr != nil {
			return aerr
		}
	}
}

// loadOnNode runs the loader for one node. While the load is in progress the
// task shows as waiting with the loader's reason; the wait is cleared once
// the load ends, whatever its result.
func (m *Manager) loadOnNode(ctx context.Context, t *Task, loader NodeLoader, node, model string) error {
	waited := false
	err := loader(ctx, node, model, func(reason string) {
		waited = true
		m.setWaiting(t, reason)
	})
	if waited {
		m.clearWaiting(t)
	}
	return err
}

// raiseNodeLoadAlert records the error that a node could not load the model.
// A nil sink records nothing.
func raiseNodeLoadAlert(sink NodeLoadAlerts, node, model string, cause error) {
	if sink == nil {
		return
	}
	title := fmt.Sprintf("Model %s failed to load on node %s", model, node)
	body := fmt.Sprintf("Node %s could not make %s resident: %v. The task moves to another node of this LLM entry, or fails when no other node is left.", node, model, cause)
	sink.Add(alerts.LevelError, title, body, "")
}

// nodeLeft reports whether any candidate node is not yet excluded. The ledger
// waits out its whole timeout when every candidate is excluded, so the
// failover checks this before it acquires again.
func nodeLeft(choices []capacity.NodeChoice, excluded []string) bool {
	for _, c := range choices {
		if !slices.Contains(excluded, c.Name) {
			return true
		}
	}
	return false
}

// requireLeasedNode refuses to start a pi-family worker that holds a capacity
// lease naming no node while its LLM entry lists compute nodes: with no node
// the session would start on pi's own default endpoint instead of on a node of
// the entry. The candidate list comes from the resolver the lease was
// requested through. With capacity off, no lease, a held node, another
// backend, or an entry that lists no nodes, it returns nil.
func (m *Manager) requireLeasedNode(t *Task, backend, model, heldNode string) error {
	if heldNode != "" || !servesProviderModels(backend) || !m.capacityOn() {
		return nil
	}
	if led := m.Capacity(); led == nil || led.LeaseOf(t.ID) == nil {
		return nil
	}
	if fn := m.CapacityChoices(); fn == nil || len(fn(backend, model)) == 0 {
		return nil
	}
	return fmt.Errorf("task %s not spawned: %s worker on model %s holds capacity but no compute node was leased, and its LLM entry lists compute nodes", t.ID, backend, model)
}
