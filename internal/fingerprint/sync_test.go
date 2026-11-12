package fingerprint

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// testPeer serves the fingerprints an exporter would export under the
// pair key derived from token, after checking the bearer — the same
// shape the real endpoint answers with, so a matching pair produces
// equal hashes by construction.
func testPeer(t *testing.T, token string, exp *Exporter) (string, func() int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if got, want := r.Header.Get("Authorization"), "Bearer "+token; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		key, err := PairKey(token)
		if err != nil {
			t.Errorf("server PairKey: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(exp.Export(key))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() int64 { return atomic.LoadInt64(&hits) }
}

// testStore returns a fresh MatchStore persisting under t.TempDir().
func testStore(t *testing.T) *MatchStore {
	t.Helper()
	s, err := NewMatchStore(filepath.Join(t.TempDir(), "matches.json"))
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	return s
}

func testSyncer(entries []multiserver.Entry, exp *Exporter, store *MatchStore) *Syncer {
	return &Syncer{
		Peers:   func() []multiserver.Entry { return entries },
		Local:   exp,
		Matches: store,
	}
}

// nodeExporter is a daemon with one resolvable local node.
func nodeExporter(name, uuid string) *Exporter {
	return &Exporter{
		Nodes: &IdentityResolver{
			Nodes: func() []compute.Node {
				return []compute.Node{{Name: name, Kind: compute.KindLocal}}
			},
			Local: func() map[string]any {
				return map[string]any{"gpu_uuids": []string{uuid}}
			},
		},
	}
}

// groupedLLM is a daemon with one LLM in the (possibly padded) group g.
func groupedLLM(name, g string) *Exporter {
	return &Exporter{
		LLMs: func() []inference.LLM {
			return []inference.LLM{{Name: name, BillingGroup: g}}
		},
	}
}

// TestSyncSkipsPeerNotOptedIn — the peer's entry has FingerprintSync
// off, so the pass makes NO request at all: silence, not a 403.
func TestSyncSkipsPeerNotOptedIn(t *testing.T) {
	// The peer would show the collision if asked — it must never be asked.
	url, hits := testPeer(t, "token-a", nodeExporter("peer-node", "uuid-9"))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: false}},
		nodeExporter("local-node", "uuid-9"),
		store,
	)
	s.SyncOnce(context.Background())
	if got := hits(); got != 0 {
		t.Fatalf("expected zero requests to a not-opted-in peer, got %d", got)
	}
	if got := len(store.List("")); got != 0 {
		t.Errorf("no records expected without a fetch, got %d", got)
	}
}

// TestSyncCreatesPendingNodeMatch — the two daemons see the same
// physical node (same GPU UUID, different local names), the keyed
// hashes coincide, and exactly one PENDING record is created.
func TestSyncCreatesPendingNodeMatch(t *testing.T) {
	url, _ := testPeer(t, "token-a", nodeExporter("peer-node", "uuid-9"))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: true}},
		nodeExporter("local-node", "uuid-9"),
		store,
	)
	s.SyncOnce(context.Background())

	recs := store.List("")
	if len(recs) != 1 {
		t.Fatalf("expected exactly one match record, got %d: %+v", len(recs), recs)
	}
	m := recs[0]
	if m.State != StatePending {
		t.Errorf("state = %q, want %q", m.State, StatePending)
	}
	if m.Peer != "peer" || m.Kind != KindNode || m.LocalName != "local-node" || m.PeerName != "peer-node" {
		t.Errorf("record = %+v, want peer %q / node %q / local %q / peer name %q",
			m, "peer", KindNode, "local-node", "peer-node")
	}
	if got, want := len(m.HashPrefix), 8; got != want {
		t.Errorf("hash prefix length = %d, want %d (display-only, never the full hash)", got, want)
	}
}

// TestSyncNoMatchNoRecord — different nodes on the two sides produce
// different keyed hashes and therefore no record at all.
func TestSyncNoMatchNoRecord(t *testing.T) {
	url, _ := testPeer(t, "token-a", nodeExporter("peer-node", "uuid-1"))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: true}},
		nodeExporter("local-node", "uuid-2"),
		store,
	)
	s.SyncOnce(context.Background())
	if got := len(store.List("")); got != 0 {
		t.Fatalf("expected no match records for disjoint nodes, got %d: %+v", got, store.List(""))
	}
}

// TestSyncRejectedStaysRejected — the operator rejects the match, and
// a later re-sync (the peer still shows the collision) must not re-open
// it: one record, still rejected.
func TestSyncRejectedStaysRejected(t *testing.T) {
	url, _ := testPeer(t, "token-a", nodeExporter("peer-node", "uuid-9"))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: true}},
		nodeExporter("local-node", "uuid-9"),
		store,
	)
	s.SyncOnce(context.Background())
	recs := store.List("")
	if len(recs) != 1 {
		t.Fatalf("expected one record before rejection, got %d", len(recs))
	}
	if _, err := store.Reject(recs[0].ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	// The collision is still on both sides; the re-sync must not touch the decision.
	s.SyncOnce(context.Background())
	recs = store.List("")
	if len(recs) != 1 {
		t.Fatalf("expected exactly one record after re-sync, got %d: %+v", len(recs), recs)
	}
	if recs[0].State != StateRejected {
		t.Errorf("state after re-sync = %q, want %q — a rejected match must stay rejected",
			recs[0].State, StateRejected)
	}
}

// TestSyncBillingGroupMatch — the same billing group on both sides
// (the peer's with padding, trimmed at export) yields one PENDING
// billing-kind record.
func TestSyncBillingGroupMatch(t *testing.T) {
	url, _ := testPeer(t, "token-a", groupedLLM("peer-llm", "  shared-plan-a "))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: true}},
		groupedLLM("claude-a", "shared-plan-a"),
		store,
	)
	s.SyncOnce(context.Background())

	recs := store.List("")
	if len(recs) != 1 {
		t.Fatalf("expected exactly one billing match, got %d: %+v", len(recs), recs)
	}
	m := recs[0]
	if m.State != StatePending || m.Kind != KindBilling {
		t.Errorf("record = %+v, want pending billing match", m)
	}
	if m.LocalName != "claude-a" || m.PeerName != "peer-llm" {
		t.Errorf("names = %q / %q, want %q / %q", m.LocalName, m.PeerName, "claude-a", "peer-llm")
	}
}

// TestSyncPeerErrorFailsSoftLoggedOnce — a failing peer (403 because
// it has not opted in on its side) is logged exactly once across
// repeated passes, and never stops the healthy peer or the loop.
func TestSyncPeerErrorFailsSoftLoggedOnce(t *testing.T) {
	downURL := func() string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}()
	upURL, upHits := testPeer(t, "token-b", nodeExporter("peer-node", "uuid-9"))

	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prev) })

	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{
			{Name: "down", URL: downURL, Token: "token-x", FingerprintSync: true},
			{Name: "up", URL: upURL, Token: "token-b", FingerprintSync: true},
		},
		nodeExporter("local-node", "uuid-9"),
		store,
	)
	// Two passes, same failure episode: still one log line, and the
	// healthy peer reconciles on both.
	s.SyncOnce(context.Background())
	s.SyncOnce(context.Background())

	if got, want := strings.Count(logBuf.String(), "peer down"), 1; got != want {
		t.Errorf("failure log lines = %d, want %d (once per failure episode):\n%s", got, want, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "[fingerprint]") {
		t.Errorf("failure line must carry the [fingerprint] prefix:\n%s", logBuf.String())
	}
	if got := upHits(); got != 2 {
		t.Errorf("healthy peer hits = %d, want 2 — a peer failure must not stop the others", got)
	}
	recs := store.List("")
	if len(recs) != 1 || recs[0].State != StatePending || recs[0].Peer != "up" {
		t.Errorf("records = %+v, want one pending match from the healthy peer", recs)
	}
}

// TestSyncIntervalDefaultIsSlow — the default cadence is the slow
// exchange (>= 5 minutes), not a fast poll.
func TestSyncIntervalDefaultIsSlow(t *testing.T) {
	s := &Syncer{}
	if got := s.interval(); got != DefaultSyncInterval {
		t.Errorf("default interval = %s, want %s", got, DefaultSyncInterval)
	}
	if DefaultSyncInterval < 5*time.Minute {
		t.Errorf("DefaultSyncInterval = %s, must be at least 5 minutes", DefaultSyncInterval)
	}
}

// TestSyncNeverConfirms — the exchange records pending suggestions and
// stops: after a matching sync nothing is confirmed, the ledger-facing
// SharedWith view is empty, and sync.go itself carries no confirming
// call.
func TestSyncNeverConfirms(t *testing.T) {
	url, _ := testPeer(t, "token-a", nodeExporter("peer-node", "uuid-9"))
	store := testStore(t)
	s := testSyncer(
		[]multiserver.Entry{{Name: "peer", URL: url, Token: "token-a", FingerprintSync: true}},
		nodeExporter("local-node", "uuid-9"),
		store,
	)
	s.SyncOnce(context.Background())

	if got := len(store.List(StateConfirmed)); got != 0 {
		t.Errorf("confirmed records = %d, want 0 — this file must never confirm", got)
	}
	if got := store.List(StatePending); len(got) != 1 {
		t.Fatalf("pending records = %d, want 1", len(got))
	}
	if got := store.SharedWith(KindNode, "local-node"); len(got) != 0 {
		t.Errorf("SharedWith = %d records, want none — only confirmed matches may reach the ledger", len(got))
	}
	src, err := os.ReadFile("sync.go")
	if err != nil {
		t.Fatalf("read sync.go: %v", err)
	}
	if strings.Contains(string(src), ".Confirm(") {
		t.Error("sync.go must not contain a confirming code path")
	}
}

// TestSyncAfterPassFiresPerPass — AfterPass runs once per pass of Run
// (including the immediate first one), and a slow hook only delays the
// loop: it neither panics nor stops it.
func TestSyncAfterPassFiresPerPass(t *testing.T) {
	var calls int64
	s := testSyncer(nil, nodeExporter("gpu1", "GPU-1"), testStore(t))
	s.Interval = 10 * time.Millisecond
	s.AfterPass = func(ctx context.Context) {
		atomic.AddInt64(&calls, 1)
		time.Sleep(25 * time.Millisecond) // slower than the interval
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&calls) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := atomic.LoadInt64(&calls); n < 3 {
		t.Fatalf("AfterPass calls = %d, want at least 3 (first pass plus ticks)", n)
	}

	// Nil hook: unchanged behavior.
	s2 := testSyncer(nil, nodeExporter("gpu1", "GPU-1"), testStore(t))
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	s2.Run(ctx2)
}
