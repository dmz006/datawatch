package fingerprint

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
)

// newPeerGate builds a gate with one node match to peer "peer-a" (whose
// node is named "peer-node") wired against the daemon at url; LocalHeld
// reports held/limit for the "node:gpu-0" pool. The match is confirmed
// only when holdsConfirmed, and the peer is opted in only when
// holdsOptedIn. Each request the peer's daemon receives is counted in
// *hits.
func newPeerGate(t *testing.T, url string, holdsConfirmed, holdsOptedIn bool, held, limit int, hits *int32) *PeerGate {
	t.Helper()
	s := newTestStore(t)
	m, created := s.Observe("peer-a", KindNode, "gpu-0", "peer-node", testHash)
	if !created {
		t.Fatalf("Observe: created = false, want true")
	}
	if holdsConfirmed {
		if _, err := s.Confirm(m.ID); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
	}
	return &PeerGate{
		Matches: s,
		Peers: func() []PeerRef {
			return []PeerRef{{Name: "peer-a", URL: url, Token: "tok", OptedIn: holdsOptedIn}}
		},
		LocalHeld: func(pool string) (int, int) {
			if pool != "node:gpu-0" {
				t.Fatalf("LocalHeld called with pool %q, want %q", pool, "node:gpu-0")
			}
			return held, limit
		},
	}
}

// serveCapacity answers /api/capacity in the shape internal/
// server/capacity.go writes, reporting the given held count on the peer
// node pool.
func serveCapacity(held int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		body := `{"pools":[{"name":"node:peer-node","held":` + strconv.Itoa(held) + `,"limit":4}]}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}
}

// failablePeerServer is a capacity peer whose operator can flip it down
// and back up mid-test; hits, when set, counts every request received.
type failablePeerServer struct {
	*httptest.Server
	fail atomic.Bool
	hits *int32
}

func newFailablePeerServer(t *testing.T, hits *int32) *failablePeerServer {
	s := &failablePeerServer{hits: hits}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hits != nil {
			atomic.AddInt32(s.hits, 1)
		}
		if s.fail.Load() {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		serveCapacity(1)(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func countLogLines(buf *bytes.Buffer) int {
	n := 0
	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if bytes.Contains(line, []byte("[fingerprint]")) {
			n++
		}
	}
	return n
}

func TestPeerGateUnreachableFailsOpen(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		serveCapacity(9)(w, r)
	}))
	url := srv.URL
	srv.Close() // same URL, no listener: every request fails to connect

	// The local node alone fits under its limit; only the peer's (never
	// reachable) usage would have pushed it over.
	g := newPeerGate(t, url, true, true, 0, 1, &hits)
	if got := g.Reason("gpu-0"); got != "" {
		t.Fatalf("unreachable peer must fail open, got reason %q", got)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("requests reached the dead peer: %d, want 0", got)
	}
}

func TestPeerGateLogsOncePerEpisode(t *testing.T) {
	var hits int32
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr) // restore; never log.SetOutput(nil)

	srv := newFailablePeerServer(t, &hits)
	g := newPeerGate(t, srv.URL, true, true, 0, 4, nil)

	srv.fail.Store(true)
	if got := g.Reason("gpu-0"); got != "" {
		t.Fatalf("failure must fail open, got %q", got)
	}
	g.Reason("gpu-0")
	g.Reason("gpu-0")
	if got := countLogLines(&logBuf); got != 1 {
		t.Fatalf("three failures in one episode logged %d times, want 1", got)
	}

	srv.fail.Store(false) // recovery clears the episode
	if got := g.Reason("gpu-0"); got != "" {
		t.Fatalf("healthy peer below limit must admit, got %q", got)
	}
	if got := countLogLines(&logBuf); got != 1 {
		t.Fatalf("a success must not log, got %d [fingerprint] lines", got)
	}
	if got := atomic.LoadInt32(&hits); got != 4 {
		t.Fatalf("requests = %d, want 4", got)
	}

	srv.fail.Store(true) // a new episode logs afresh
	g.Reason("gpu-0")
	if got := countLogLines(&logBuf); got != 2 {
		t.Fatalf("second episode logged, %d total lines, want 2", got)
	}
}

func TestPeerGateBlocksWhenCombinedHeldReachesLimit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		serveCapacity(1)(w, r)
	}))
	defer srv.Close()

	g := newPeerGate(t, srv.URL, true, true, 1, 2, &hits)
	want := "shared node busy on peer peer-a: 2 of 2 in use across both daemons"
	if got := g.Reason("gpu-0"); got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("requests = %d, want exactly 1", got)
	}
}

func TestPeerGateIgnoresPendingMatch(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		serveCapacity(9)(w, r)
	}))
	defer srv.Close()

	g := newPeerGate(t, srv.URL, false, true, 0, 1, &hits)
	if got := g.Reason("gpu-0"); got != "" {
		t.Fatalf("pending match must not block, got %q", got)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("pending match made %d HTTP calls, want 0", got)
	}
}

func TestPeerGateIgnoresPeerNotOptedIn(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		serveCapacity(9)(w, r)
	}))
	defer srv.Close()

	g := newPeerGate(t, srv.URL, true, false, 0, 1, &hits)
	if got := g.Reason("gpu-0"); got != "" {
		t.Fatalf("peer not opted in must not block, got %q", got)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("not-opted-in peer got %d HTTP calls, want 0", got)
	}
}
