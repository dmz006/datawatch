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
// first refresh every node counts as healthy.
type nodeHealthView struct {
	mu     sync.Mutex
	loaded bool
	nodes  map[string]*compute.Node
	now    func() time.Time
}

func newNodeHealthView() *nodeHealthView {
	return &nodeHealthView{now: func() time.Time { return time.Now().UTC() }}
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
		return true
	}
	n, ok := v.nodes[name]
	return ok && !n.InMaintenance(v.now())
}
