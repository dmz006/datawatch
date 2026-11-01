package main

import (
	"sync"
	"time"

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

// refresh replaces the copied node set.
func (v *nodeHealthView) refresh(nodes []*compute.Node) {
	m := make(map[string]*compute.Node, len(nodes))
	for _, n := range nodes {
		if n != nil {
			m[n.Name] = n
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
