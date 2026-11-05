package server

// Pull-merge tests: pulled peers (observer.PullCollector) must appear in
// the observer peer views with a source + pull object, merged with push
// peers and with shadow rows (federated peers with observer_pull off).
// All tests use an in-memory fake PullFetcher and a fixed clock — no
// network, no real time.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fixedNow is a deterministic clock pinned to a single instant.
type fixedNow struct {
	mu sync.Mutex
	t  time.Time
}

func newFixedNow() *fixedNow {
	return &fixedNow{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
}

func (f *fixedNow) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// fakePullFetcher serves per-peer snapshots or errors in memory.
type fakePullFetcher struct {
	mu    sync.Mutex
	snaps map[string]*observer.StatsResponse
	errs  map[string]error
}

func newFakePullFetcher() *fakePullFetcher {
	return &fakePullFetcher{snaps: map[string]*observer.StatsResponse{}, errs: map[string]error{}}
}

func (f *fakePullFetcher) Fetch(_ context.Context, name string) (*observer.StatsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[name]; err != nil {
		return nil, err
	}
	return f.snaps[name], nil
}

// wirePullCollector attaches a PullCollector driven by fetcher to s and
// seeds its target set from the peer store.
func wirePullCollector(t *testing.T, s *Server, fetcher observer.PullFetcher) *observer.PullCollector {
	t.Helper()
	reg := &testTickers{}
	col := observer.NewPullCollector(observer.PullCollectorOptions{
		Fetcher:   fetcher,
		NewTicker: reg.new,
		Now:       newFixedNow().Now,
	})
	op := &observerPull{s: s, col: col}
	s.observerPull = op
	t.Cleanup(col.Stop)
	op.col.SetTargets(op.targets())
	return col
}

// waitForPullSuccess blocks until the collector recorded a successful
// fetch for name.
func waitForPullSuccess(t *testing.T, col *observer.PullCollector, name string) {
	t.Helper()
	waitFor(t, 2*time.Second, "initial pull of "+name, func() bool {
		st, ok := col.Last(name)
		return ok && !st.LastSuccessAt.IsZero()
	})
}

// pullRowWire is the pull object as it appears on the wire.
type pullRowWire struct {
	Enabled       bool       `json:"enabled"`
	Stale         bool       `json:"stale"`
	StaleReason   string     `json:"stale_reason"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	IntervalS     int        `json:"interval_s"`
}

// pullRow is one observer peer row (the fields under test).
type pullRow struct {
	Name         string      `json:"name"`
	Shape        string      `json:"shape"`
	Source       string      `json:"source"`
	Shadow       bool        `json:"shadow"`
	ComputeNode  string      `json:"compute_node"`
	LastPushAt   string      `json:"last_push_at"`
	Pull         pullRowWire `json:"pull"`
	RegisteredAt time.Time   `json:"registered_at"`
}

// getPeerRows calls GET /api/observer/peers and decodes the rows.
func getPeerRows(t *testing.T, s *Server) []pullRow {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/observer/peers = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Peers []pullRow `json:"peers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Peers
}

func rowsByName(rows []pullRow, name string) []pullRow {
	var out []pullRow
	for _, r := range rows {
		if r.Name == name {
			out = append(out, r)
		}
	}
	return out
}

// getEnvelopesByPeer calls GET /api/observer/envelopes/all-peers and
// returns the by_peer map.
func getEnvelopesByPeer(t *testing.T, s *Server) map[string][]map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/envelopes/all-peers", nil)
	s.handleObserverEnvelopesAllPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET all-peers = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		ByPeer map[string][]map[string]any `json:"by_peer"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.ByPeer
}

func TestObserverPullMerge_PushOnly(t *testing.T) {
	reg := newFakePeerRegistry()
	if _, err := reg.Register("pushpeer", "B", "v9.9", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	s := &Server{peerRegistry: reg}

	rows := getPeerRows(t, s)
	mine := rowsByName(rows, "pushpeer")
	if len(mine) != 1 {
		t.Fatalf("want exactly one pushpeer row, got %d (%v)", len(mine), rows)
	}
	row := mine[0]
	if row.Source != "push" {
		t.Errorf("source = %q, want push", row.Source)
	}
	if row.Shadow {
		t.Errorf("shadow = true, want false")
	}
	if row.Pull.Enabled {
		t.Errorf("pull.enabled = true, want false (not configured for pull)")
	}
	if row.Pull.Stale {
		t.Errorf("pull.stale = true, want false")
	}
	if row.Pull.StaleReason != "none" {
		t.Errorf("pull.stale_reason = %q, want none", row.Pull.StaleReason)
	}
	if row.Pull.LastSuccessAt != nil || row.Pull.LastAttemptAt != nil {
		t.Errorf("pull timestamps = (%v, %v), want both nil", row.Pull.LastSuccessAt, row.Pull.LastAttemptAt)
	}
	// Pre-existing fields are preserved.
	if row.ComputeNode != "" {
		t.Errorf("compute_node = %q, want empty (no compute registry)", row.ComputeNode)
	}
}

func TestObserverPullMerge_PullOnly(t *testing.T) {
	s := newObserverPullServer(t, "http://unused", "tok", nil) // peer1 configured for pull
	s.peerRegistry = newFakePeerRegistry()
	s.observerAPI = &fakeObserverForEnrich{}

	ff := newFakePullFetcher()
	ff.snaps["peer1"] = &observer.StatsResponse{
		V: 2,
		Envelopes: []observer.Envelope{
			{ID: "session:env1", Kind: observer.EnvelopeSession, Label: "pulled"},
		},
	}
	col := wirePullCollector(t, s, ff)
	waitForPullSuccess(t, col, "peer1")

	rows := getPeerRows(t, s)
	mine := rowsByName(rows, "peer1")
	if len(mine) != 1 {
		t.Fatalf("want exactly one peer1 row, got %d (%v)", len(mine), rows)
	}
	row := mine[0]
	if row.Source != "pull" {
		t.Errorf("source = %q, want pull", row.Source)
	}
	if row.Shadow {
		t.Errorf("shadow = true, want false")
	}
	if !row.Pull.Enabled {
		t.Errorf("pull.enabled = false, want true")
	}
	if row.Pull.Stale {
		t.Errorf("pull.stale = true, want false")
	}
	if row.Pull.StaleReason != "none" {
		t.Errorf("pull.stale_reason = %q, want none", row.Pull.StaleReason)
	}
	if row.Pull.LastSuccessAt == nil {
		t.Errorf("pull.last_success_at = nil, want set")
	}
	// newObserverPullServer sets ObserverPullIntervalS=30 on the entry.
	if row.Pull.IntervalS != 30 {
		t.Errorf("pull.interval_s = %d, want 30", row.Pull.IntervalS)
	}

	// The pulled snapshot feeds the all-peers envelope view.
	byPeer := getEnvelopesByPeer(t, s)
	envs, ok := byPeer["peer1"]
	if !ok {
		t.Fatalf("by_peer missing peer1: %v", byPeer)
	}
	if len(envs) != 1 {
		t.Fatalf("by_peer[peer1] has %d envelopes, want 1", len(envs))
	}
	if got := envs[0]["label"]; got != "pulled" {
		t.Errorf("envelope label = %v, want pulled", got)
	}
}

func TestObserverPushAndPullShowsOnce(t *testing.T) {
	s := newObserverPullServer(t, "http://unused", "tok", nil) // peer1 configured for pull
	// peer1 ALSO pushes, but its push payload is absent: the envelope
	// data must come from the pull snapshot.
	reg := newFakePeerRegistry()
	if _, err := reg.Register("peer1", "B", "v9.9", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.peerRegistry = reg
	s.observerAPI = &fakeObserverForEnrich{}

	ff := newFakePullFetcher()
	ff.snaps["peer1"] = &observer.StatsResponse{
		V: 2,
		Envelopes: []observer.Envelope{
			{ID: "session:env2", Kind: observer.EnvelopeSession, Label: "dual"},
		},
	}
	col := wirePullCollector(t, s, ff)
	waitForPullSuccess(t, col, "peer1")

	rows := getPeerRows(t, s)
	mine := rowsByName(rows, "peer1")
	if len(mine) != 1 {
		t.Fatalf("want exactly one peer1 row (push+pull merged), got %d (%v)", len(mine), rows)
	}
	row := mine[0]
	if row.Source != "push" {
		t.Errorf("source = %q, want push (the push row wins)", row.Source)
	}
	if row.Shadow {
		t.Errorf("shadow = true, want false")
	}
	if !row.Pull.Enabled {
		t.Errorf("pull.enabled = false, want true (pull configured)")
	}
	if row.Pull.LastSuccessAt == nil {
		t.Errorf("pull.last_success_at = nil, want set")
	}

	// The pull snapshot is preferred over the (absent) push payload.
	byPeer := getEnvelopesByPeer(t, s)
	envs, ok := byPeer["peer1"]
	if !ok || len(envs) != 1 {
		t.Fatalf("by_peer[peer1] = %v, want the one pulled envelope", byPeer)
	}
	if got := envs[0]["label"]; got != "dual" {
		t.Errorf("envelope label = %v, want dual", got)
	}
}

func TestObserverShadowRowsExcludedFromAggregates(t *testing.T) {
	s := newObserverPullServer(t, "http://unused", "tok", nil) // peer1: pull on
	// shadowpeer: federated but pull off — a shadow row.
	if err := s.serverStore.Add(multiserverEntryShadow()); err != nil {
		t.Fatalf("add shadow entry: %v", err)
	}
	s.peerRegistry = newFakePeerRegistry()
	s.observerAPI = &fakeObserverForEnrich{}

	ff := newFakePullFetcher()
	ff.snaps["peer1"] = &observer.StatsResponse{
		V: 2,
		Envelopes: []observer.Envelope{
			{ID: "session:env3", Kind: observer.EnvelopeSession, Label: "real"},
		},
	}
	col := wirePullCollector(t, s, ff)
	waitForPullSuccess(t, col, "peer1")

	// The list view shows both — peer1 as a pull row, shadowpeer as a
	// shadow row.
	rows := getPeerRows(t, s)
	if got := len(rowsByName(rows, "peer1")); got != 1 {
		t.Errorf("peer1 rows = %d, want 1 (%v)", got, rows)
	}
	shadow := rowsByName(rows, "shadowpeer")
	if len(shadow) != 1 {
		t.Fatalf("want exactly one shadowpeer row, got %d (%v)", len(shadow), rows)
	}
	if shadow[0].Source != "pull" {
		t.Errorf("shadowpeer source = %q, want pull", shadow[0].Source)
	}
	if !shadow[0].Shadow {
		t.Errorf("shadowpeer shadow = false, want true")
	}
	if shadow[0].Pull.Enabled {
		t.Errorf("shadowpeer pull.enabled = true, want false")
	}
	if shadow[0].Pull.LastSuccessAt != nil {
		t.Errorf("shadowpeer pull.last_success_at set, want nil (never pulled)")
	}

	// The all-peers envelope view includes the pulled peer and EXCLUDES
	// the shadow row.
	byPeer := getEnvelopesByPeer(t, s)
	if _, ok := byPeer["peer1"]; !ok {
		t.Errorf("by_peer missing peer1: %v", byPeer)
	}
	if _, ok := byPeer["shadowpeer"]; ok {
		t.Errorf("by_peer contains shadowpeer — shadow rows must be excluded: %v", byPeer)
	}

	// Meta-peers: the shadow row stays out of every bucket; the pulled
	// peer lands in unbound (no bound ComputeNode).
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/federation/meta-peers", nil)
	s.handleFederationMetaPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("meta-peers = %d: %s", rr.Code, rr.Body.String())
	}
	var meta struct {
		ByNode map[string]struct {
			Observers []struct {
				Peer   string `json:"peer"`
				Source string `json:"source"`
			} `json:"observers"`
		} `json:"by_node"`
		Unbound []struct {
			Peer   string `json:"peer"`
			Source string `json:"source"`
		} `json:"unbound"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	foundUnbound := false
	for _, e := range meta.Unbound {
		if e.Peer == "peer1" {
			foundUnbound = true
			if e.Source != "pull" {
				t.Errorf("meta unbound peer1 source = %q, want pull", e.Source)
			}
		}
		if e.Peer == "shadowpeer" {
			t.Errorf("meta unbound contains shadowpeer — must be excluded")
		}
	}
	if !foundUnbound {
		t.Errorf("meta unbound missing peer1: %v", meta.Unbound)
	}
	for node, b := range meta.ByNode {
		for _, e := range b.Observers {
			if e.Peer == "shadowpeer" {
				t.Errorf("meta by_node[%s] contains shadowpeer — must be excluded", node)
			}
		}
	}
}

func TestObserverPullStaleReportsReason(t *testing.T) {
	s := newObserverPullServer(t, "http://unused", "tok", nil)
	s.peerRegistry = newFakePeerRegistry()

	ff := newFakePullFetcher()
	// A timeout error — the collector classifies it as "timeout" and
	// marks the target stale.
	ff.errs["peer1"] = context.DeadlineExceeded
	col := wirePullCollector(t, s, ff)
	waitFor(t, 2*time.Second, "stale state for peer1", func() bool {
		st, ok := col.Last("peer1")
		return ok && st.Stale
	})

	rows := getPeerRows(t, s)
	mine := rowsByName(rows, "peer1")
	if len(mine) != 1 {
		t.Fatalf("want exactly one peer1 row, got %d (%v)", len(mine), rows)
	}
	row := mine[0]
	if !row.Pull.Stale {
		t.Errorf("pull.stale = false, want true")
	}
	if row.Pull.StaleReason != "timeout" {
		t.Errorf("pull.stale_reason = %q, want timeout", row.Pull.StaleReason)
	}
	if !row.Pull.Enabled {
		t.Errorf("pull.enabled = false, want true (still configured)")
	}
	if row.Pull.LastAttemptAt == nil {
		t.Errorf("pull.last_attempt_at = nil, want set")
	}
}

func TestObserverUnboundPulledPeerInFreeList(t *testing.T) {
	s := newObserverPullServer(t, "http://unused", "tok", nil)
	s.peerRegistry = newFakePeerRegistry()

	ff := newFakePullFetcher()
	ff.snaps["peer1"] = &observer.StatsResponse{
		V: 2,
		Envelopes: []observer.Envelope{
			{ID: "session:env4", Kind: observer.EnvelopeSession, Label: "free"},
		},
	}
	col := wirePullCollector(t, s, ff)
	waitForPullSuccess(t, col, "peer1")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers/free", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("free = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Peers []pullRow `json:"peers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *pullRow
	for i := range out.Peers {
		if out.Peers[i].Name == "peer1" {
			found = &out.Peers[i]
		}
	}
	if found == nil {
		t.Fatalf("free list missing unbound pulled peer peer1: %v", out.Peers)
	}
	if found.Source != "pull" {
		t.Errorf("free peer1 source = %q, want pull", found.Source)
	}
	if found.ComputeNode != "" {
		t.Errorf("free peer1 compute_node = %q, want empty (unbound)", found.ComputeNode)
	}
}

// multiserverEntryShadow builds the shadow-peer fixture: a federated peer
// with observer_pull disabled.
func multiserverEntryShadow() *multiserver.Entry {
	return &multiserver.Entry{
		Name:         "shadowpeer",
		URL:          "http://shadow.invalid",
		Enabled:      true,
		Federated:    true,
		ObserverPull: false,
		Capabilities: []string{federation.CapObserversRead},
	}
}
