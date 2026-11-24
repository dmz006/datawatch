package main

// The federation model for capacity. Each host has its OWN local caps: a
// peer's llm:<entry> and node:<name> pools are its own, they add to the
// federated capacity and never count against this daemon's local pools. The
// one pool that can span hosts is the Claude account. A peer's leases count
// against this daemon's account:claude pool only when
//   - an operator CONFIRMED a billing_group fingerprint match saying the peer's
//     LLM entry bills to the same account as a local entry (the peer's own
//     leases on that matched entry count), or
//   - the operator opted the peer in through a claude-code entry's budget
//     peer_window (the peer's whole account:claude pool counts, as before).
// A pending match counts nothing and is reported as a possible shared account
// awaiting the operator's confirmation; a rejected one counts nothing. A peer
// with a separate account adds its own. Nothing here assumes this host's
// numbers: every peer is shown at the limits it reports.
//
// Peers are read through the fingerprint PeerUsageCache (one GET
// /api/capacity per opted-in or window peer, refreshed on the 10 s tick). An
// unreachable peer counts 0 and is shown unreachable (fail open).

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
)

type (
	fedHostShare = server.CapacityHost
	fedView      = server.CapacityFederation
)

const possibleSharedAccountNote = "possible shared account, awaiting your confirmation"

type federationAccount struct {
	gate   func() *fingerprint.PeerGate
	window func() []string
	snap   atomic.Pointer[fedSnapshot]
}

// fedSnapshot is the in-memory view Held and View read, rebuilt by Refresh.
// The match and peer stores hold their locks across file writes, so the ledger
// (which calls Held under its own lock) must not read them directly.
type fedSnapshot struct {
	window    map[string]bool
	peers     map[string]fingerprint.PeerRef
	confirmed map[string][]string // peer -> distinct peer-side entry names
	pending   []fingerprint.Match
	pendingBy map[string]bool
}

func newFederationAccount(gate func() *fingerprint.PeerGate, window func() []string) *federationAccount {
	return &federationAccount{gate: gate, window: window}
}

// Refresh rebuilds the snapshot and refreshes the usage of every peer that is
// opted in for fingerprint sync or named in a peer_window. It makes no call for
// any other peer.
func (f *federationAccount) Refresh(ctx context.Context) {
	g := f.gate()
	if g == nil || g.Matches == nil || g.Peers == nil {
		return
	}
	s := &fedSnapshot{window: map[string]bool{}, peers: map[string]fingerprint.PeerRef{}, confirmed: map[string][]string{}, pendingBy: map[string]bool{}}
	if f.window != nil {
		for _, p := range f.window() {
			s.window[p] = true
		}
	}
	for _, p := range g.Peers() {
		s.peers[p.Name] = p
	}
	seen := map[string]bool{}
	for _, m := range g.Matches.List("") {
		if m.Kind != fingerprint.KindBilling {
			continue
		}
		switch m.State {
		case fingerprint.StateConfirmed:
			if key := m.Peer + "\x00" + m.PeerName; !seen[key] {
				seen[key] = true
				s.confirmed[m.Peer] = append(s.confirmed[m.Peer], m.PeerName)
			}
		case fingerprint.StatePending:
			s.pending = append(s.pending, m)
			s.pendingBy[m.Peer] = true
		}
	}
	f.snap.Store(s)
	if g.Usage == nil {
		return
	}
	var wg sync.WaitGroup
	for _, p := range s.peers {
		if !p.OptedIn && !s.window[p.Name] {
			continue
		}
		wg.Add(1)
		go func(p fingerprint.PeerRef) {
			defer wg.Done()
			_ = g.Usage.Refresh(ctx, p)
		}(p)
	}
	wg.Wait()
}

// Held is what the peers sharing this daemon's Claude account hold, from the
// last refresh. It reads only memory.
func (f *federationAccount) Held() int {
	s, g := f.snap.Load(), f.gate()
	if s == nil || g == nil || g.Usage == nil {
		return 0
	}
	n := 0
	for peer := range s.window {
		if h, ok := g.Usage.Held(peer, accountClaudePool); ok {
			n += h
		}
	}
	for peer, entries := range s.confirmed {
		if s.window[peer] {
			continue // counted once, through the whole account pool
		}
		for _, e := range entries {
			if h, ok := g.Usage.Held(peer, "llm:"+e); ok {
				n += h
			}
		}
	}
	return n
}

// External is the ledger's external-count hook for the account pool only; this
// host's own pools are never touched by a peer.
func (f *federationAccount) External() func(pool string) int {
	return func(pool string) int {
		if pool == accountClaudePool {
			return f.Held()
		}
		return 0
	}
}

func hostTotals(h *fedHostShare) {
	for _, p := range h.Pools {
		if !strings.HasPrefix(p.Name, "llm:") && !strings.HasPrefix(p.Name, "node:") {
			continue
		}
		llm := strings.HasPrefix(p.Name, "llm:")
		if p.Limit > 0 {
			if llm {
				h.LLMLimit += p.Limit
			} else {
				h.NodeLimit += p.Limit
			}
		} else {
			h.UnlimitedPools++
		}
		if llm {
			h.LLMHeld += p.Held
		} else {
			h.NodeHeld += p.Held
		}
	}
}

// View is the cluster view: this host first, then every peer that is opted in,
// in a peer_window or matched, each at its own pools and sizes, plus the
// per-host sums and the account sharing. Nothing here uses this host's numbers
// for a peer.
func (f *federationAccount) View(selfName string, local []capacity.PoolStatus, accountLimit, localAccountHeld int) fedView {
	var v fedView
	self := fedHostShare{Name: selfName, Self: true, Reachable: true, SharedAccount: "self"}
	for _, p := range local {
		self.Pools = append(self.Pools, fingerprint.PoolUsage{Name: p.Name, Limit: p.Limit, Held: p.Held, External: p.External, Over: p.Over})
	}
	hostTotals(&self)
	v.Hosts = append(v.Hosts, self)
	s, g := f.snap.Load(), f.gate()
	v.Account = server.CapacityAccountShare{Limit: accountLimit, LocalHeld: localAccountHeld}
	if s != nil && g != nil {
		names := make([]string, 0, len(s.peers))
		for name, p := range s.peers {
			if p.OptedIn || s.window[name] || len(s.confirmed[name]) > 0 || s.pendingBy[name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			h := fedHostShare{Name: name, SharedAccount: "none"}
			if g.Usage != nil {
				h.Pools, h.Reachable = g.Usage.Pools(name)
			}
			hostTotals(&h)
			switch {
			case s.window[name]:
				h.SharedAccount = "opted_in"
				if n, ok := g.Usage.Held(name, accountClaudePool); ok {
					h.CountedHeld = n
				}
			case len(s.confirmed[name]) > 0:
				h.SharedAccount = "confirmed"
				for _, e := range s.confirmed[name] {
					if n, ok := g.Usage.Held(name, "llm:"+e); ok {
						h.CountedHeld += n
					}
				}
			case s.pendingBy[name]:
				h.SharedAccount = "pending"
				h.Note = possibleSharedAccountNote
			}
			v.Account.PeerHeld += h.CountedHeld
			v.Hosts = append(v.Hosts, h)
		}
		for _, m := range s.pending {
			v.PendingSharedAccounts = append(v.PendingSharedAccounts, server.CapacityPendingShare{
				ID: m.ID, Peer: m.Peer, LocalEntry: m.LocalName, PeerEntry: m.PeerName,
				Message: possibleSharedAccountNote + ": " + m.Peer + " entry " + m.PeerName + " has the same billing group as local entry " + m.LocalName,
			})
		}
	}
	v.Account.Total = v.Account.LocalHeld + v.Account.PeerHeld
	for _, h := range v.Hosts {
		v.Cluster.LLMLimit += h.LLMLimit
		v.Cluster.LLMHeld += h.LLMHeld
		v.Cluster.NodeLimit += h.NodeLimit
		v.Cluster.NodeHeld += h.NodeHeld
		v.Cluster.UnlimitedPools += h.UnlimitedPools
	}
	return v
}

// claudeAccountPeers lists the peers opted in through the budget peer_window of
// any claude-code entry, sorted, each once.
func claudeAccountPeers(registry func() *inference.Registry) func() []string {
	return func() []string {
		reg := registry()
		if reg == nil {
			return nil
		}
		seen := map[string]bool{}
		for _, l := range reg.List() {
			if l.Kind != inference.KindClaudeCode || l.Budget == nil {
				continue
			}
			for _, peer := range l.Budget.PeerWindow {
				if peer != "" {
					seen[peer] = true
				}
			}
		}
		out := make([]string, 0, len(seen))
		for peer := range seen {
			out = append(out, peer)
		}
		sort.Strings(out)
		return out
	}
}
