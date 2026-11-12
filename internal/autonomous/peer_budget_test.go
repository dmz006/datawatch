package autonomous

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// fakePeer is an httptest peer answering GET /api/budget/usage. It records
// how many calls it received and the last query it saw.
type fakePeer struct {
	srv    *httptest.Server
	calls  atomic.Int32
	status int
	spend  float64

	mu    sync.Mutex
	query map[string]string
	auth  string
}

func newFakePeer(t *testing.T, spend float64) *fakePeer {
	t.Helper()
	p := &fakePeer{status: http.StatusOK, spend: spend}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.calls.Add(1)
		p.mu.Lock()
		p.auth = r.Header.Get("Authorization")
		p.query = map[string]string{}
		for _, k := range []string{"llm", "from", "to"} {
			p.query[k] = r.URL.Query().Get(k)
		}
		p.mu.Unlock()
		if r.URL.Path != "/api/budget/usage" {
			http.NotFound(w, r)
			return
		}
		if p.status != http.StatusOK {
			http.Error(w, "boom", p.status)
			return
		}
		_, _ = fmt.Fprintf(w, `{"llm":"claude","spend_usd":%v}`, p.spend)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// peerHarness wires a ledger and a PeerBudget over one budgeted LLM "claude"
// with a $10 rolling window, a fake clock and the given peers registered.
func peerHarness(t *testing.T, window []string, peers map[string]*fakePeer) (*BudgetLedger, *PeerBudget, *budgetFakeClock) {
	t.Helper()
	clk := &budgetFakeClock{t: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	bw := &inference.BudgetWindow{CeilingUSD: 10, Hours: 5, PeerWindow: window}
	l := newTestBudgetLedger(t, clk, bw)
	pb := &PeerBudget{
		Clock:   clk.Now,
		Budgets: l.Budgets,
		LLMs:    l.LLMs,
		Lookup: func(name string) (string, string, bool) {
			p, ok := peers[name]
			if !ok {
				return "", "", false
			}
			return p.srv.URL, "tok-" + name, true
		},
		KnownPeers: func() []string {
			var names []string
			for n := range peers {
				names = append(names, n)
			}
			return names
		},
	}
	l.PeerSpend = pb.Spend
	l.PeerBreakdown = pb.Breakdown
	return l, pb, clk
}

func TestPeerBudgetOptInCounts(t *testing.T) {
	peer := newFakePeer(t, 6)
	l, pb, clk := peerHarness(t, []string{"gpu-box"}, map[string]*fakePeer{"gpu-box": peer})
	l.Record("claude", clk.Now(), 4, 1, 1)

	// Before any refresh the cache is empty, so only local spend counts.
	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("blocked before the peer cache was filled")
	}

	pb.Refresh(context.Background())

	blocked, reason := l.Blocked("claude")
	if !blocked {
		t.Fatalf("local $4 + peer $6 should reach the $10 ceiling; status=%+v", mustStatus(t, l))
	}
	t.Logf("reason: %s", reason)
	st := mustStatus(t, l)
	if st.SpendUSD != 4 || st.PeerSpendUSD != 6 {
		t.Fatalf("spend split = local %v peer %v, want 4 and 6", st.SpendUSD, st.PeerSpendUSD)
	}
	if len(st.Peers) != 1 || st.Peers[0].Peer != "gpu-box" || !st.Peers[0].Counted || st.Peers[0].SpendUSD != 6 {
		t.Fatalf("breakdown = %+v", st.Peers)
	}

	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.auth != "Bearer tok-gpu-box" {
		t.Errorf("Authorization = %q", peer.auth)
	}
	wantFrom := clk.Now().Add(-5 * time.Hour).UTC().Format(time.RFC3339)
	wantTo := clk.Now().UTC().Format(time.RFC3339)
	if peer.query["llm"] != "claude" || peer.query["from"] != wantFrom || peer.query["to"] != wantTo {
		t.Errorf("query = %v, want llm=claude from=%s to=%s", peer.query, wantFrom, wantTo)
	}
}

func TestPeerBudgetIgnoredWhenNotOptedIn(t *testing.T) {
	peer := newFakePeer(t, 50)
	l, pb, clk := peerHarness(t, nil, map[string]*fakePeer{"gpu-box": peer})
	l.Record("claude", clk.Now(), 4, 1, 1)

	pb.Refresh(context.Background())

	if n := peer.calls.Load(); n != 0 {
		t.Fatalf("peer not in peer_window got %d HTTP calls, want 0", n)
	}
	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("a peer that is not opted in must not count")
	}
	st := mustStatus(t, l)
	if st.PeerSpendUSD != 0 {
		t.Fatalf("PeerSpendUSD = %v, want 0", st.PeerSpendUSD)
	}
	if len(st.Peers) != 1 || st.Peers[0].Peer != "gpu-box" || st.Peers[0].Counted || st.Peers[0].Reason != "not opted in" {
		t.Fatalf("breakdown = %+v", st.Peers)
	}
}

func TestPeerBudgetConfirmerCanVeto(t *testing.T) {
	peer := newFakePeer(t, 50)
	l, pb, clk := peerHarness(t, []string{"gpu-box"}, map[string]*fakePeer{"gpu-box": peer})
	pb.Confirm = func(peer, llm string) bool { return false }
	l.Record("claude", clk.Now(), 1, 1, 1)

	pb.Refresh(context.Background())

	if n := peer.calls.Load(); n != 0 {
		t.Fatalf("vetoed peer got %d HTTP calls, want 0", n)
	}
	st := mustStatus(t, l)
	if st.PeerSpendUSD != 0 || len(st.Peers) != 1 || st.Peers[0].Counted || st.Peers[0].Reason != "not confirmed" {
		t.Fatalf("status = %+v", st)
	}
}

func TestPeerBudgetUnreachableFailsOpen(t *testing.T) {
	failing := newFakePeer(t, 50)
	failing.status = http.StatusInternalServerError
	dead := newFakePeer(t, 50)
	dead.srv.Close() // connection refused

	l, pb, clk := peerHarness(t, []string{"failing", "dead", "missing"},
		map[string]*fakePeer{"failing": failing, "dead": dead})
	l.Record("claude", clk.Now(), 4, 1, 1)

	done := make(chan struct{})
	go func() { pb.Refresh(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * peerFetchTimeout):
		t.Fatal("Refresh stalled on unreachable peers")
	}

	if blocked, _ := l.Blocked("claude"); blocked {
		t.Fatal("unreachable peers must fail open")
	}
	st := mustStatus(t, l)
	if st.PeerSpendUSD != 0 {
		t.Fatalf("PeerSpendUSD = %v, want 0", st.PeerSpendUSD)
	}
	reasons := map[string]string{}
	for _, p := range st.Peers {
		if p.Counted || p.SpendUSD != 0 {
			t.Errorf("peer %s counted: %+v", p.Peer, p)
		}
		reasons[p.Peer] = p.Reason
	}
	if reasons["failing"] != "unreachable: HTTP 500" || reasons["dead"] != "unreachable" || reasons["missing"] != "not registered" {
		t.Fatalf("reasons = %v", reasons)
	}

	// A peer that recovers is counted on the next refresh.
	failing.status = http.StatusOK
	failing.spend = 6
	pb.Refresh(context.Background())
	if blocked, _ := l.Blocked("claude"); !blocked {
		t.Fatalf("recovered peer not counted: %+v", mustStatus(t, l))
	}
}

func TestPeerBudgetFailureLoggedOncePerEpisode(t *testing.T) {
	peer := newFakePeer(t, 6)
	peer.status = http.StatusServiceUnavailable
	_, pb, _ := peerHarness(t, []string{"gpu-box"}, map[string]*fakePeer{"gpu-box": peer})

	key := peerKey("claude", "gpu-box")
	pb.Refresh(context.Background())
	pb.Refresh(context.Background())
	if !pb.failing[key] {
		t.Fatal("failure episode not tracked")
	}
	peer.status = http.StatusOK
	pb.Refresh(context.Background())
	if pb.failing[key] {
		t.Fatal("failure episode not cleared after recovery")
	}
}

func TestPeerBudgetLocalOnlyNoLoop(t *testing.T) {
	peer := newFakePeer(t, 6)
	l, pb, clk := peerHarness(t, []string{"gpu-box"}, map[string]*fakePeer{"gpu-box": peer})
	l.Record("claude", clk.Now(), 4, 1, 1)
	pb.Refresh(context.Background())

	// The peer spend counts toward Blocked, but the ledger's own spend
	// figures, which are what this daemon serves to a peer asking for its
	// usage, stay local-only.
	if blocked, _ := l.Blocked("claude"); !blocked {
		t.Fatal("peer spend should count toward the ceiling")
	}
	if got := l.SpendInWindow("claude"); got != 4 {
		t.Fatalf("SpendInWindow = %v, want local-only 4", got)
	}
	if got := l.SpendBetween("claude", clk.Now().Add(-5*time.Hour), clk.Now()); got != 4 {
		t.Fatalf("SpendBetween = %v, want local-only 4", got)
	}
}

func TestPeerWindowConfirmerDefault(t *testing.T) {
	c := PeerWindowConfirmer(func(llm string) []string {
		if llm == "claude" {
			return []string{"a"}
		}
		return nil
	})
	if !c("a", "claude") || c("b", "claude") || c("a", "other") {
		t.Fatal("default confirmer must confirm only peers listed in the LLM's peer_window")
	}
	if PeerWindowConfirmer(nil)("a", "claude") {
		t.Fatal("nil peer_window source must confirm nothing")
	}
}

func mustStatus(t *testing.T, l *BudgetLedger) BudgetStatus {
	t.Helper()
	st, ok := l.Status("claude")
	if !ok {
		t.Fatal("no budget status for claude")
	}
	return st
}
