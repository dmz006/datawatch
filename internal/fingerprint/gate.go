// The gate.go half of fingerprint reconciliation
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c): at
// admission time a CONFIRMED shared-node match makes the peer's usage of
// the same underlying node count against the local limit, so the two
// daemons behave as if they share one pool. Design open question 13: a
// peer that cannot be reached must never stall local work — its failure
// is logged once per episode and it contributes zero, so admission fails
// open.
package fingerprint

import (
	"fmt"
	"sync/atomic"
)

// PeerRef is one federated peer as the gate sees it: its name (matching
// Match.Peer), the base URL of its daemon, the bearer token to present,
// and whether its operator opted it in for fingerprint sync. The wiring
// task adapts the multiserver entries to this shape; the gate imports no
// multiserver package.
type PeerRef struct {
	Name    string
	URL     string
	Token   string
	OptedIn bool
}

// capacityPoolView is everything the gate reads out of a peer's
// /api/capacity body (the shape internal/server/capacity.go writes); the
// rest of the response is ignored.
type capacityPoolView struct {
	Name string `json:"name"`
	Held int    `json:"held"`
}

// PeerGate is the admission-time check: for a local node, each confirmed
// shared peer's held count on its "node:"+<peer name> pool is added to
// the local held count before the local limit is tested. The peer
// counts come from Usage, an in-memory cache refreshed in the background,
// so the gate itself never makes a network call.
type PeerGate struct {
	Matches   *MatchStore
	Peers     func() []PeerRef
	LocalHeld func(pool string) (held, limit int)
	Usage     *PeerUsageCache // cached peer usage; nil = peers contribute zero

	// view is the in-memory copy of the peer list and the confirmed node
	// matches that ReasonWithHeld reads once RefreshView has run. The peer
	// store and the match store both hold their locks across file writes, so
	// the ledger (which calls ReasonWithHeld under its own lock) must not
	// read them directly.
	view atomic.Pointer[gateView]
}

// gateView is an immutable snapshot taken by RefreshView.
type gateView struct {
	peers  map[string]PeerRef
	shared map[string][]Match // local node name -> confirmed node matches
}

// RefreshView re-reads the peer list and the confirmed node matches into the
// in-memory view ReasonWithHeld uses. Call it off the admission path (at
// startup and after each slow sync pass); a decision or peer change takes
// effect at the next refresh.
func (g *PeerGate) RefreshView() {
	if g.Matches == nil || g.Peers == nil {
		return
	}
	v := &gateView{peers: map[string]PeerRef{}, shared: map[string][]Match{}}
	for _, p := range g.Peers() {
		v.peers[p.Name] = p
	}
	for _, m := range g.Matches.List(StateConfirmed) {
		if m.Kind == KindNode {
			v.shared[m.LocalName] = append(v.shared[m.LocalName], m)
		}
	}
	g.view.Store(v)
}

// Reason returns "" to admit the named local node, or a wait reason when
// a confirmed shared peer's usage pushes the combined held count to the
// local limit. It never returns a block for a peer that could not be
// reached: such a peer contributes zero.
func (g *PeerGate) Reason(node string) string {
	if g.LocalHeld == nil {
		return ""
	}
	held, limit := g.LocalHeld("node:" + node)
	return g.ReasonWithHeld(node, held, limit)
}

// ReasonWithHeld is Reason with the local held count and limit supplied by
// the caller. The capacity ledger uses it because it already holds its own
// lock when it asks the gate, so the gate must not read the ledger back.
func (g *PeerGate) ReasonWithHeld(node string, held, limit int) string {
	if g.Matches == nil || g.Peers == nil {
		return ""
	}
	var peers map[string]PeerRef
	var shared []Match
	if v := g.view.Load(); v != nil {
		peers, shared = v.peers, v.shared[node]
	} else {
		// No view yet (RefreshView never ran): read the live stores.
		peers = make(map[string]PeerRef)
		for _, p := range g.Peers() {
			peers[p.Name] = p
		}
		shared = g.Matches.SharedWith(KindNode, node)
	}
	for _, m := range shared {
		peer, ok := peers[m.Peer]
		if !ok || !peer.OptedIn {
			continue // unresolved or not opted in
		}
		if limit <= 0 {
			continue // 0 = unlimited, same convention as the ledger
		}
		if g.Usage == nil {
			continue // no cache: fail open
		}
		peerHeld, ok := g.Usage.Held(peer.Name, "node:"+m.PeerName)
		if !ok || g.Usage.stale(peer.Name) {
			g.Usage.Request(peer) // a later call sees fresh data
		}
		if !ok {
			continue // missing or expired: contributes zero
		}
		if combined := held + peerHeld; combined >= limit {
			return fmt.Sprintf("shared node busy on peer %s: %d of %d in use across both daemons", peer.Name, combined, limit)
		}
	}
	return ""
}
