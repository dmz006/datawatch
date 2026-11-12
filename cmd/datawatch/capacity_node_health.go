package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// nodeHealthView is the ledger's NodeHealthy callback state. The compute
// registry persists to disk under its own lock, so the ledger (which calls the
// hook under its lock) never reads it: the sync tick copies the nodes here and
// the hook reads the copy.
//
// A node is healthy when the registry holds it and no maintenance window
// covers now. The window is checked at call time against the copied record, so
// a window opening or closing between ticks takes effect at once; only a node
// added to or removed from the registry waits for the next tick. Before the
// first refresh every node counts as healthy, unless the view is strict (see
// newStrictNodeHealthView): the daemon's own view is, so a restart cannot place
// work on a node it has not read from the registry yet.
type nodeHealthView struct {
	mu     sync.Mutex
	loaded bool
	// strict makes every node unhealthy until the first refresh.
	strict bool
	nodes  map[string]*compute.Node
	now    func() time.Time
}

func newNodeHealthView() *nodeHealthView {
	return &nodeHealthView{now: func() time.Time { return time.Now().UTC() }}
}

// newStrictNodeHealthView is a view that reports no node healthy until the
// first refresh has copied the registry. The daemon uses it so that, after a
// restart, no request is placed on a node the ledger has not loaded yet.
func newStrictNodeHealthView() *nodeHealthView {
	v := newNodeHealthView()
	v.strict = true
	return v
}

// refresh replaces the copied node set. Each record is copied by value, so
// the view keeps its own copy of the drain fields and permissions: it never
// reads a registry entry in place, and a later registry edit of the same node
// does not change a record the view already copied.
func (v *nodeHealthView) refresh(nodes []*compute.Node) {
	m := make(map[string]*compute.Node, len(nodes))
	for _, n := range nodes {
		if n != nil {
			cp := *n
			m[n.Name] = &cp
		}
	}
	v.mu.Lock()
	v.nodes, v.loaded = m, true
	v.mu.Unlock()
}

// healthy reports whether name may take new work.
func (v *nodeHealthView) healthy(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.loaded {
		return !v.strict
	}
	n, ok := v.nodes[name]
	return ok && !n.InMaintenance(v.now())
}

// gate reports whether name may take new work for the named consumer, and
// the reason when it may not. It is the ledger's NodeGate: the drain and
// maintenance states and the node's consumer permissions are each evaluated
// at call time against the copied record, so a drain, an undrain, a window
// opening or closing, or a permission edit takes effect the next time the
// ledger checks the node — no refresh tick is needed. The order is drained,
// in maintenance, consumer denied: drain wins over everything else, and the
// consumer check runs last so drain and window reasons are never masked by an
// RBAC refusal.
//
// The loaded and strict rules are the healthy() rules: before the first
// refresh the node is open unless the view is strict, in which case it is
// denied ("node list not loaded yet"), and a name the registry does not hold
// is denied ("node not in the compute registry"). An empty consumer means no
// consumer check, matching capacity.Request's empty-Consumer contract.
func (v *nodeHealthView) gate(name, consumer string) (bool, string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.loaded {
		if !v.strict {
			return true, ""
		}
		return false, "node list not loaded yet"
	}
	n, ok := v.nodes[name]
	if !ok {
		return false, "node not in the compute registry"
	}
	if n.DrainActive(v.now()) {
		return false, "drained"
	}
	if n.InMaintenance(v.now()) {
		return false, "in maintenance"
	}
	if consumer != "" && !n.AllowsConsumer(consumer) {
		return false, fmt.Sprintf("consumer %s denied", consumer)
	}
	return true, ""
}

// state reports name's drain, maintenance and consumer-denial state for the
// pool snapshot; it is the ledger's Options.NodeState. Like the gate, every
// field is evaluated at call time against the copied record, so a drain, an
// undrain, a window opening or closing, or a permission edit takes effect
// the next time the snapshot reads the node — no refresh tick is needed. It
// is a plain in-memory read, as the ledger's lock rule requires. Before the
// first refresh, and for a name the registry does not hold, it reports the
// zero state: not drained, not in maintenance, no denials.
func (v *nodeHealthView) state(name string) capacity.NodeState {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.loaded {
		return capacity.NodeState{}
	}
	n, ok := v.nodes[name]
	if !ok {
		return capacity.NodeState{}
	}
	ns := capacity.NodeState{
		Drained:         n.DrainActive(v.now()),
		InMaintenance:   n.InMaintenance(v.now()),
		DeniedConsumers: append([]string(nil), n.Permissions.DeniedConsumers...),
	}
	if ns.Drained && !n.DrainUntil.IsZero() {
		ns.DrainUntil = n.DrainUntil
	}
	return ns
}
