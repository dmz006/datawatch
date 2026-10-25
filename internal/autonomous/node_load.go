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

// loadWorkerNode runs right after a worker lease is granted. For an LLM entry
// that pools two or more nodes, it makes the model resident on the node the
// lease landed on. A node that fails is alerted, the lease is released, and
// the lease is acquired again with that node excluded. The exclusion lasts
// only for this attempt. When no other node is left, the load error is
// returned, as a failed start is.
//
// A single-node entry has no load step: it returns nil without touching the
// loader.
func (m *Manager) loadWorkerNode(ctx context.Context, prd *PRD, t *Task, backend, model string) error {
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
		if aerr := m.acquireLease(ctx, prd, t, backend, model, capacity.RoleWorker, excluded); aerr != nil {
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
