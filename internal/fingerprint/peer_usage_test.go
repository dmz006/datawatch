package fingerprint

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeerUsageCacheHitReturnsHeld(t *testing.T) {
	srv := httptest.NewServer(serveCapacity(3))
	defer srv.Close()
	peer := PeerRef{Name: "peer-a", URL: srv.URL, Token: "tok", OptedIn: true}
	c := &PeerUsageCache{}
	if err := c.Refresh(context.Background(), peer); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if n, ok := c.Held("peer-a", "node:peer-node"); !ok || n != 3 {
		t.Fatalf("Held = %d,%v want 3,true", n, ok)
	}
	if n, ok := c.Held("peer-a", "node:other"); ok || n != 0 {
		t.Fatalf("unreported pool = %d,%v want 0,false", n, ok)
	}
}

func TestPeerUsageMissingEntryReturnsZero(t *testing.T) {
	c := &PeerUsageCache{}
	if n, ok := c.Held("nobody", "node:x"); ok || n != 0 {
		t.Fatalf("Held = %d,%v want 0,false", n, ok)
	}
}

func TestPeerUsageEntryOlderThanMaxAgeIsMissing(t *testing.T) {
	srv := httptest.NewServer(serveCapacity(2))
	defer srv.Close()
	now := time.Unix(1000, 0)
	c := &PeerUsageCache{TTL: time.Second, MaxAge: 5 * time.Second, Now: func() time.Time { return now }}
	if err := c.Refresh(context.Background(), PeerRef{Name: "p", URL: srv.URL, Token: "tok", OptedIn: true}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if n, ok := c.Held("p", "node:peer-node"); !ok || n != 2 {
		t.Fatalf("at MaxAge Held = %d,%v want 2,true", n, ok)
	}
	now = now.Add(time.Second)
	if n, ok := c.Held("p", "node:peer-node"); ok || n != 0 {
		t.Fatalf("past MaxAge Held = %d,%v want 0,false", n, ok)
	}
}

func TestPeerUsageRequestCoalescesConcurrentCallers(t *testing.T) {
	var hits int32
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-gate
		serveCapacity(1)(w, r)
	}))
	defer srv.Close()
	peer := PeerRef{Name: "p", URL: srv.URL, Token: "tok", OptedIn: true}
	c := &PeerUsageCache{}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Request(peer) }()
	}
	wg.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&hits) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	c.Request(peer) // still in flight: no second request
	if err := c.Refresh(context.Background(), peer); err != nil {
		t.Fatalf("coalesced Refresh: %v", err)
	}
	close(gate)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := c.Held("p", "node:peer-node"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n, ok := c.Held("p", "node:peer-node"); !ok || n != 1 {
		t.Fatalf("Held = %d,%v want 1,true", n, ok)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("requests = %d, want exactly 1", got)
	}
	c.Request(peer) // fresh entry: nothing to do
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("fresh entry triggered a request: %d", got)
	}
}

func TestPeerUsageSlowPeerDoesNotDelayGate(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		serveCapacity(1)(w, r)
	}))
	defer srv.Close()
	defer close(release)

	g := newPeerGate(t, srv.URL, true, true, 1, 2, nil)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if got := g.ReasonWithHeld("gpu-0", 1, 2); got != "" {
			t.Fatalf("slow peer must contribute zero, got %q", got)
		}
		if _, ok := g.Usage.Held("peer-a", "node:peer-node"); ok {
			t.Fatal("Held reported data for an unanswered peer")
		}
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("calls took %v, want well under a second", d)
	}
}

func TestPeerUsageGateBlocksOnceCacheIsFresh(t *testing.T) {
	srv := httptest.NewServer(serveCapacity(1))
	defer srv.Close()
	g := newPeerGate(t, srv.URL, true, true, 1, 2, nil)
	if got := g.ReasonWithHeld("gpu-0", 1, 2); got != "" {
		t.Fatalf("cold cache must admit, got %q", got)
	}
	// The background fetch the first call started lands shortly after.
	want := "shared node busy on peer peer-a: 2 of 2 in use across both daemons"
	deadline := time.Now().Add(2 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		if got = g.ReasonWithHeld("gpu-0", 1, 2); got != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func TestPeerUsageGateNilUsageFailsOpen(t *testing.T) {
	g := newPeerGate(t, "http://127.0.0.1:1", true, true, 5, 2, nil)
	g.Usage = nil
	if got := g.ReasonWithHeld("gpu-0", 1, 2); got != "" {
		t.Fatalf("nil Usage must fail open, got %q", got)
	}
}

func TestPeerUsageBadResponsesContributeZeroAndLogOnce(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	var mode atomic.Int32 // 0 = non-200, 1 = garbage body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 0 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	peer := PeerRef{Name: "p", URL: srv.URL, OptedIn: true}
	c := &PeerUsageCache{}

	for i := 0; i < 2; i++ {
		if err := c.Refresh(context.Background(), peer); err == nil {
			t.Fatal("non-200 must return an error")
		}
	}
	mode.Store(1)
	if err := c.Refresh(context.Background(), peer); err == nil {
		t.Fatal("bad body must return an error")
	}
	if n, ok := c.Held("p", "node:peer-node"); ok || n != 0 {
		t.Fatalf("Held = %d,%v want 0,false", n, ok)
	}
	if got := countLogLines(&logBuf); got != 1 {
		t.Fatalf("one episode logged %d lines, want 1", got)
	}
}

func TestPeerUsageRefreshAllSkipsPeersNotOptedIn(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		serveCapacity(1)(w, r)
	}))
	defer srv.Close()
	c := &PeerUsageCache{Peers: func() []PeerRef {
		return []PeerRef{{Name: "p", URL: srv.URL, Token: "tok", OptedIn: false}}
	}}
	c.RefreshAll(context.Background())
	c.Request(PeerRef{Name: "p", URL: srv.URL, Token: "tok", OptedIn: false})
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("not-opted-in peer got %d requests, want 0", got)
	}
}
