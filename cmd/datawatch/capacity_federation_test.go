package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/inference"
)

type fakePeer struct {
	srv *httptest.Server
}

// newFakePeer is a federated daemon that reports its own pools, each at its
// own size.
func newFakePeer(t *testing.T, poolsJSON string) *fakePeer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capacity" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"pools":` + poolsJSON + `}`))
	}))
	t.Cleanup(srv.Close)
	return &fakePeer{srv}
}

type fedFixture struct {
	acct    *federationAccount
	matches *fingerprint.MatchStore
}

func newFedFixture(t *testing.T, peers map[string]*fakePeer, window []string) *fedFixture {
	t.Helper()
	matches, err := fingerprint.NewMatchStore("")
	if err != nil {
		t.Fatal(err)
	}
	refs := func() []fingerprint.PeerRef {
		var out []fingerprint.PeerRef
		for name, p := range peers {
			out = append(out, fingerprint.PeerRef{Name: name, URL: p.srv.URL, Token: "tok", OptedIn: true})
		}
		return out
	}
	gate := &fingerprint.PeerGate{Matches: matches, Peers: refs, Usage: &fingerprint.PeerUsageCache{Peers: refs}}
	acct := newFederationAccount(func() *fingerprint.PeerGate { return gate }, func() []string { return window })
	return &fedFixture{acct: acct, matches: matches}
}

func (f *fedFixture) observe(t *testing.T, peer, local, theirs string, confirm bool) {
	t.Helper()
	m, _ := f.matches.Observe(peer, fingerprint.KindBilling, local, theirs, "abcdef0123456789")
	if confirm {
		if _, err := f.matches.Confirm(m.ID); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	sepAccountPools  = `[{"name":"llm:claude-code","limit":1,"held":1},{"name":"account:claude","limit":4,"held":1},{"name":"llm:pi","limit":9,"held":4}]`
	sameAccountPools = `[{"name":"llm:claude-code","limit":1,"held":1},{"name":"llm:claude-lean","limit":1,"held":0},{"name":"account:claude","limit":2,"held":1},{"name":"llm:pi","limit":3,"held":2}]`
	pendingAcctPools = `[{"name":"llm:claude-code","limit":1,"held":1},{"name":"account:claude","limit":2,"held":1}]`
)

// A peer on a separate Claude account adds its own capacity: nothing it holds
// counts against this daemon's account:claude pool.
func TestFederationSeparateAccountPeerCountsNothing(t *testing.T) {
	f := newFedFixture(t, map[string]*fakePeer{"sep": newFakePeer(t, sepAccountPools)}, nil)
	f.acct.Refresh(context.Background())
	if got := f.acct.Held(); got != 0 {
		t.Fatalf("held = %d, want 0: a separate account is not this account", got)
	}
}

// Two peers: one confirmed same account (its own lease on the matched entry
// counts, nothing else of it), one pending (counts nothing, is reported).
func TestFederationConfirmedSharesAndPendingCountsNothing(t *testing.T) {
	f := newFedFixture(t, map[string]*fakePeer{
		"same":    newFakePeer(t, sameAccountPools),
		"pending": newFakePeer(t, pendingAcctPools),
		"sep":     newFakePeer(t, sepAccountPools),
	}, nil)
	f.observe(t, "same", "claude-code", "claude-code", true) // confirmed: same billing account
	f.observe(t, "pending", "claude-code", "claude-code", false)
	f.acct.Refresh(context.Background())
	if got := f.acct.Held(); got != 1 {
		t.Fatalf("held = %d, want 1: only the confirmed peer's lease on the matched entry (llm:claude-code)", got)
	}
	// The peer's other claude entry is not the matched one: not counted.
	f.observe(t, "same", "claude-lean", "claude-lean", true)
	f.acct.Refresh(context.Background())
	if got := f.acct.Held(); got != 1 {
		t.Fatalf("held = %d, want 1 (claude-lean on the peer holds 0)", got)
	}
	v := f.acct.View("self", nil, 2, 1)
	var same, pending, sep *fedHostShare
	for i := range v.Hosts {
		switch v.Hosts[i].Name {
		case "same":
			same = &v.Hosts[i]
		case "pending":
			pending = &v.Hosts[i]
		case "sep":
			sep = &v.Hosts[i]
		}
	}
	if same == nil || same.SharedAccount != "confirmed" || same.CountedHeld != 1 {
		t.Fatalf("same = %+v", same)
	}
	if pending == nil || pending.SharedAccount != "pending" || pending.CountedHeld != 0 || !strings.Contains(pending.Note, "possible shared account, awaiting your confirmation") {
		t.Fatalf("pending = %+v", pending)
	}
	if sep == nil || sep.SharedAccount != "none" || sep.CountedHeld != 0 {
		t.Fatalf("sep = %+v", sep)
	}
	if len(v.PendingSharedAccounts) != 1 || v.PendingSharedAccounts[0].Peer != "pending" {
		t.Fatalf("pending list = %+v", v.PendingSharedAccounts)
	}
}

// A rejected match counts nothing, whatever the peer holds.
func TestFederationRejectedMatchCountsNothing(t *testing.T) {
	f := newFedFixture(t, map[string]*fakePeer{"same": newFakePeer(t, sameAccountPools)}, nil)
	m, _ := f.matches.Observe("same", fingerprint.KindBilling, "claude-code", "claude-code", "ab")
	_, _ = f.matches.Confirm(m.ID)
	_, _ = f.matches.Reject(m.ID)
	f.acct.Refresh(context.Background())
	if got := f.acct.Held(); got != 0 {
		t.Fatalf("held = %d, want 0 after a reject", got)
	}
}

// A peer opted in through peer_window counts its whole account pool, as it does
// today, and a peer both opted in and matched is counted once.
func TestFederationPeerWindowPeerCountsWholeAccountPoolOnce(t *testing.T) {
	peers := map[string]*fakePeer{"win": newFakePeer(t, sameAccountPools)}
	f := newFedFixture(t, peers, []string{"win"})
	f.observe(t, "win", "claude-code", "claude-code", true)
	f.acct.Refresh(context.Background())
	if got := f.acct.Held(); got != 1 {
		t.Fatalf("held = %d, want 1 (account:claude held 1 on the peer, counted once)", got)
	}
	v := f.acct.View("self", nil, 2, 1)
	if len(v.Hosts) < 2 || v.Hosts[1].SharedAccount != "opted_in" {
		t.Fatalf("hosts = %+v, want the peer shown as opted in", v.Hosts)
	}
}

// Local pools stay per host: a peer's llm: and node: pools never count against
// this daemon's own llm: pools, each host is listed at its own size, and the
// cluster line sums them.
func TestFederationLocalPoolsStayPerHostAndSum(t *testing.T) {
	f := newFedFixture(t, map[string]*fakePeer{
		"big":   newFakePeer(t, sameAccountPools), // llm:pi limit 3 held 2
		"other": newFakePeer(t, sepAccountPools),  // llm:pi limit 9 held 4
	}, nil)
	f.observe(t, "big", "claude-code", "claude-code", true)
	f.acct.Refresh(context.Background())
	// The external hook the ledger uses for this host's own pools is untouched.
	external := f.acct.External()
	if external("llm:pi") != 0 || external("llm:claude-code") != 0 || external("host") != 0 || external("node:gpu") != 0 {
		t.Fatal("a peer's local pools must never count against this daemon's local pools")
	}
	if external(accountClaudePool) != 1 {
		t.Fatalf("account external = %d, want 1", external(accountClaudePool))
	}
	local := []capacity.PoolStatus{{Name: "llm:pi", Limit: 1, Held: 1}, {Name: "node:local", Limit: 2, Held: 0}}
	v := f.acct.View("self", local, 2, 1)
	if v.Hosts[0].Name != "self" || !v.Hosts[0].Self || v.Hosts[0].LLMLimit != 1 || v.Hosts[0].LLMHeld != 1 || v.Hosts[0].NodeLimit != 2 {
		t.Fatalf("self host = %+v", v.Hosts[0])
	}
	got := map[string]fedHostShare{}
	for _, h := range v.Hosts {
		got[h.Name] = h
	}
	if got["big"].LLMLimit != 5 || got["big"].LLMHeld != 2+1 { // claude-code 1 + pi 3 (+ claude-lean 1) limits: 1+1+3
		// limits: claude-code 1 + claude-lean 1 + pi 3 = 5; held 1+0+2 = 3
		t.Fatalf("big = %+v, want llm limit 5 held 3 at its own size", got["big"])
	}
	if got["other"].LLMLimit != 10 || got["other"].LLMHeld != 5 {
		t.Fatalf("other = %+v, want llm limit 10 held 5 at its own size", got["other"])
	}
	if v.Cluster.LLMLimit != 1+5+10 || v.Cluster.LLMHeld != 1+3+5 {
		t.Fatalf("cluster = %+v, want the per-host sums", v.Cluster)
	}
}

// An unreachable peer is shown unreachable, counts 0 and never blocks.
func TestFederationUnreachablePeerFailsOpen(t *testing.T) {
	p := newFakePeer(t, sameAccountPools)
	f := newFedFixture(t, map[string]*fakePeer{"gone": p}, nil)
	f.observe(t, "gone", "claude-code", "claude-code", true)
	p.srv.Close()
	f.acct.Refresh(context.Background())
	if f.acct.Held() != 0 {
		t.Fatal("an unreachable peer counts 0")
	}
	v := f.acct.View("self", nil, 2, 1)
	if len(v.Hosts) < 2 || v.Hosts[1].Reachable {
		t.Fatalf("hosts = %+v, want the peer shown unreachable", v.Hosts)
	}
	_ = time.Second
}

// The peers opted in through a claude-code entry's budget peer_window come from
// the claude-code entries only, sorted, each once.
func TestClaudeAccountPeersComeFromClaudeEntriesPeerWindow(t *testing.T) {
	reg := inference.NewRegistry()
	for _, l := range []inference.LLM{
		{Name: "claude-code", Kind: inference.KindClaudeCode, Budget: &inference.BudgetWindow{CeilingUSD: 10, Hours: 24, PeerWindow: []string{"peer-b", "peer-a"}}},
		{Name: "claude-lean", Kind: inference.KindClaudeCode, Budget: &inference.BudgetWindow{CeilingUSD: 10, Hours: 24, PeerWindow: []string{"peer-b"}}},
		{Name: "pi", Kind: inference.KindPi, Budget: &inference.BudgetWindow{CeilingUSD: 10, Hours: 24, PeerWindow: []string{"peer-c"}}},
	} {
		l := l
		if err := reg.Add(&l); err != nil {
			t.Fatal(err)
		}
	}
	got := claudeAccountPeers(func() *inference.Registry { return reg })()
	if len(got) != 2 || got[0] != "peer-a" || got[1] != "peer-b" {
		t.Fatalf("peers = %v, want [peer-a peer-b]", got)
	}
}
