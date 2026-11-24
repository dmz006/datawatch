package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

func peerCapacityServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capacity" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newPeerAccount(peers []string, lookup func(string) (string, string, bool)) *peerAccountPool {
	return &peerAccountPool{peers: func() []string { return peers }, lookup: lookup}
}

// A peer that shares the Claude account contributes its held account:claude
// leases, so the account cap counts every machine drawing on it.
func TestPeerAccountPoolCountsPeerLeases(t *testing.T) {
	srv := peerCapacityServer(t, `{"pools":[{"name":"host","held":5},{"name":"account:claude","held":1,"external":7}]}`, 200)
	p := newPeerAccount([]string{"peer-a"}, func(n string) (string, string, bool) { return srv.URL, "tok", n == "peer-a" })
	p.Refresh(context.Background())
	if got := p.Held(); got != 1 {
		t.Fatalf("held = %d, want 1: only the peer's own leases count, not its external figure", got)
	}
	// The count is the account pool's external figure on this daemon's ledger.
	led := capacity.New(capacity.Options{External: func(pool string) int {
		if pool == accountClaudePool {
			return p.Held()
		}
		return 0
	}, PollInterval: 10 * time.Millisecond})
	led.SetLimit(accountClaudePool, 2)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "a", Pools: []string{accountClaudePool}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	err := led.Acquire(context.Background(), capacity.Request{Holder: "b", Pools: []string{accountClaudePool}}, 40*time.Millisecond, nil, nil)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("second worker err = %v, want wait timeout: one local lease plus one on the peer fills the account cap of 2", err)
	}
}

// An unreachable or malformed peer counts 0 (it fails open, and says so once).
func TestPeerAccountPoolFailsOpen(t *testing.T) {
	bad := peerCapacityServer(t, `not json`, 200)
	down := peerCapacityServer(t, `{}`, 500)
	p := newPeerAccount([]string{"bad", "down", "missing"}, func(n string) (string, string, bool) {
		switch n {
		case "bad":
			return bad.URL, "tok", true
		case "down":
			return down.URL, "tok", true
		}
		return "", "", false
	})
	p.Refresh(context.Background())
	if p.Held() != 0 {
		t.Fatalf("held = %d, want 0 from peers that did not answer", p.Held())
	}
}

// The peers counted are the ones a claude-code entry opted in through its
// budget peer window; no other peer is contacted.
func TestClaudeAccountPeersComeFromClaudeEntriesPeerWindow(t *testing.T) {
	reg := inference.NewRegistry()
	for _, l := range []inference.LLM{
		{Name: "claude-code", Kind: inference.KindClaudeCode, Budget: &inference.BudgetWindow{CeilingUSD: 10, Hours: 24, PeerWindow: []string{"peer-a", "peer-b"}}},
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
