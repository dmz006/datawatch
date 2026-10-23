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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
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
// the local held count before the local limit is tested. A confirmed
// match with an opted-in peer is the only thing that makes an HTTP call;
// pending and rejected matches and peers that are not opted in make none
// at all.
type PeerGate struct {
	Matches   *MatchStore
	Peers     func() []PeerRef
	LocalHeld func(pool string) (held, limit int)
	Client    *http.Client // nil = a client with a 3-second timeout

	mu          sync.Mutex
	unreachable map[string]bool // peers currently in a failure episode
}

// Reason returns "" to admit the named local node, or a wait reason when
// a confirmed shared peer's usage pushes the combined held count to the
// local limit. It never returns a block for a peer that could not be
// reached: such a peer contributes zero.
func (g *PeerGate) Reason(node string) string {
	if g.Matches == nil || g.Peers == nil || g.LocalHeld == nil {
		return ""
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	peers := make(map[string]PeerRef)
	for _, p := range g.Peers() {
		peers[p.Name] = p
	}
	held, limit := g.LocalHeld("node:" + node)
	for _, m := range g.Matches.SharedWith(KindNode, node) {
		peer, ok := peers[m.Peer]
		if !ok || !peer.OptedIn {
			continue // unresolved or not opted in: no HTTP call
		}
		if limit <= 0 {
			continue // 0 = unlimited, same convention as the ledger
		}
		peerHeld, ok := g.pollPeer(client, peer, "node:"+m.PeerName)
		if !ok {
			continue // unreachable or bad body: contributes zero
		}
		if combined := held + peerHeld; combined >= limit {
			return fmt.Sprintf("shared node busy on peer %s: %d of %d in use across both daemons", peer.Name, combined, limit)
		}
	}
	return ""
}

// pollPeer asks peer for its capacity and returns the held count of the
// named pool. Any failure — connection error, non-200, unparseable body,
// or the pool missing — is reported through failOnce and contributes zero.
func (g *PeerGate) pollPeer(client *http.Client, peer PeerRef, pool string) (int, bool) {
	url := strings.TrimSuffix(peer.URL, "/") + "/api/capacity"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		g.failOnce(peer.Name, err)
		return 0, false
	}
	if peer.Token != "" {
		req.Header.Set("Authorization", "Bearer "+peer.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		g.failOnce(peer.Name, err)
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		g.failOnce(peer.Name, fmt.Errorf("status %d", resp.StatusCode))
		return 0, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		g.failOnce(peer.Name, err)
		return 0, false
	}
	var view struct {
		Pools []capacityPoolView `json:"pools"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		g.failOnce(peer.Name, err)
		return 0, false
	}
	for _, p := range view.Pools {
		if p.Name == pool {
			g.clearFailure(peer.Name)
			return p.Held, true
		}
	}
	g.failOnce(peer.Name, fmt.Errorf("pool %s not reported", pool))
	return 0, false
}

// failOnce marks peer as being in a failure episode and logs it the
// first time the episode is seen; repeated failures of the same peer stay
// silent until the peer answers successfully again.
func (g *PeerGate) failOnce(peer string, why error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.unreachable == nil {
		g.unreachable = make(map[string]bool)
	}
	if g.unreachable[peer] {
		return
	}
	g.unreachable[peer] = true
	log.Printf("[fingerprint] peer %s shared-pool check failed (%v); failing open, silencing until it answers again", peer, why)
}

// clearFailure ends the failure episode now that peer answered again, so
// a later failure logs afresh.
func (g *PeerGate) clearFailure(peer string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.unreachable, peer)
}
