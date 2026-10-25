// Tests for the per-compute-node usage contract in node_usage.go:
// loaded-model parsing, GPU sourcing (monitoring endpoint, peer registry,
// self observer — in that order, never guessed from a bare hostname match),
// the unreachable / pending / no-data / ok states, the non-blocking TTL
// cache, and the /api/capacity JSON shape. Everything runs against
// httptest servers only; no external network.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/observer"
)

// fakePSRegistry is the minimal PeerRegistryAPI a node-usage test needs:
// only LastPayload is consulted by node_usage.go, but the full interface
// must be implemented.
type fakePSRegistry struct {
	mu       sync.Mutex
	payloads map[string]*observer.StatsResponse
}

func (f *fakePSRegistry) Register(name, shape, version string, hostInfo map[string]any) (string, error) {
	return "token", nil
}

func (f *fakePSRegistry) Verify(name, token string) (*observer.PeerEntry, error) {
	return &observer.PeerEntry{Name: name}, nil
}

func (f *fakePSRegistry) RecordPush(name string, snap *observer.StatsResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.payloads == nil {
		f.payloads = map[string]*observer.StatsResponse{}
	}
	f.payloads[name] = snap
	return nil
}

func (f *fakePSRegistry) Get(name string) (observer.PeerEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.payloads[name] != nil {
		return observer.PeerEntry{Name: name}, true
	}
	return observer.PeerEntry{}, false
}

func (f *fakePSRegistry) LastPayload(name string) *observer.StatsResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.payloads[name]
}

func (f *fakePSRegistry) List() []observer.PeerEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []observer.PeerEntry{}
}

func (f *fakePSRegistry) Delete(name string) error { return nil }

// fakePSObserver implements ObserverAPI; only Stats() is used by
// node_usage.go (marshalled to JSON and decoded for the gpu array).
type fakePSObserver struct{ stats any }

func (f *fakePSObserver) Config() any                { return nil }
func (f *fakePSObserver) SetConfig(any) error        { return nil }
func (f *fakePSObserver) Stats() any                 { return f.stats }
func (f *fakePSObserver) Envelopes() any             { return nil }
func (f *fakePSObserver) EnvelopeTree(id string) any { return nil }
func (f *fakePSObserver) Start(ctx context.Context)  {}
func (f *fakePSObserver) Stop()                      {}
func (f *fakePSObserver) EnvelopeSummary(id string) (float64, uint64, bool) {
	return 0, 0, false
}

func gpuDoc(name string, util float64, used, total int64) []map[string]any {
	return []map[string]any{{
		"name":            name,
		"util_pct":        util,
		"mem_used_bytes":  used,
		"mem_total_bytes": total,
	}}
}

// newUsageHarness builds a bare &Server{} with a ledger, an (optionally
// empty) compute registry containing nodes, and returns both.
func newUsageHarness(t *testing.T, nodes ...compute.Node) (*Server, *compute.Registry) {
	t.Helper()
	s := &Server{}
	s.SetCapacity(capacity.New(capacity.Options{}))
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for i := range nodes {
		if err := reg.Add(&nodes[i]); err != nil {
			t.Fatalf("Add node %q: %v", nodes[i].Name, err)
		}
	}
	s.computeReg = reg
	return s, reg
}

// usageFromCapacity runs one /api/capacity request and returns the usage
// payload attached to the named pool (fatals when the pool or payload is
// missing).
func usageFromCapacity(t *testing.T, s *Server, pool string) *nodeUsage {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleCapacity: status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Pools []struct {
			Name  string     `json:"name"`
			Usage *nodeUsage `json:"usage"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal capacity body: %v (body %s)", err, rec.Body.String())
	}
	for i := range body.Pools {
		if body.Pools[i].Name == pool {
			if body.Pools[i].Usage == nil {
				t.Fatalf("pool %s has no usage payload: %s", pool, rec.Body.String())
			}
			return body.Pools[i].Usage
		}
	}
	t.Fatalf("pool %s not in /api/capacity response: %s", pool, rec.Body.String())
	return nil
}

// waitUsage calls usageFromCapacity until ok(u) is true or the deadline
// passes (the first capacity call returns the pending placeholder and only
// then a background refresh fills the entry, so callers must poll).
func waitUsage(t *testing.T, s *Server, pool string, ok func(u nodeUsage) bool) nodeUsage {
	t.Helper()
	var last nodeUsage
	deadline := time.Now().Add(10 * time.Second)
	for {
		last = *usageFromCapacity(t, s, pool)
		if ok(last) {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("pool %s never reached the expected usage; last: %+v", pool, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestNodeUsage_ParsesLoadedModel checks one resident model is parsed from
// /api/ps: name, size bytes, VRAM bytes and a concrete expires_at, with
// state ok and models_supported true.
func TestNodeUsage_ParsesLoadedModel(t *testing.T) {
	body := `{"models":[{"name":"llama3:8b","size":4600000000,"size_vram":4200000000,"expires_at":"2030-06-01T12:30:00Z"}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "modelnode", Kind: compute.KindOllama, Address: ts.URL})
	want := time.Date(2030, 6, 1, 12, 30, 0, 0, time.UTC)
	u := waitUsage(t, s, "node:modelnode", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if !u.ModelsSupported {
		t.Error("ollama node with an address: models_supported must be true")
	}
	if len(u.Models) != 1 {
		t.Fatalf("expected exactly 1 model, got %d: %+v", len(u.Models), u.Models)
	}
	m := u.Models[0]
	if m.Name != "llama3:8b" || m.SizeBytes != 4600000000 || m.SizeVRAMBytes != 4200000000 {
		t.Errorf("model parsed wrong: %+v", m)
	}
	if m.NoExpiry {
		t.Error("near-future expires_at must not be no_expiry")
	}
	if m.ExpiresAt == nil || !m.ExpiresAt.Equal(want) {
		t.Errorf("expires_at = %v, want %v (NoExpiry=%v)", m.ExpiresAt, want, m.NoExpiry)
	}
	if u.GPU == nil || len(u.GPU) != 0 {
		t.Errorf("gpu must be an empty (non-nil) array when no observer data, got %v", u.GPU)
	}
}

// TestNodeUsage_ReportsAllLoadedModels checks every model /api/ps reports
// is kept, including one without a name that falls back to the model field.
func TestNodeUsage_ReportsAllLoadedModels(t *testing.T) {
	body := `{"models":[
    {"name":"zephyr:7b","size":4000000000,"size_vram":3900000000,"expires_at":"2031-01-01T00:00:00Z"},
    {"model":"qwen2:1.5b","size":1200000000,"size_vram":1100000000,"expires_at":"2031-01-02T00:00:00Z"}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "twonodes", Kind: compute.KindOllama, Address: ts.URL})
	u := waitUsage(t, s, "node:twonodes", func(u nodeUsage) bool { return u.State == nodeUsageStateOK && len(u.Models) == 2 })
	names := map[string]bool{}
	for _, m := range u.Models {
		names[m.Name] = true
	}
	if !names["zephyr:7b"] || !names["qwen2:1.5b"] {
		t.Fatalf("both loaded models must be reported, got: %+v", u.Models)
	}
}

// TestNodeUsage_TreatsFarFutureOrZeroExpiryAsNoExpiry checks expires_at
// values at/beyond the far-future cutoff, the zero time, and an empty
// string all map to no_expiry=true with a nil expires_at.
func TestNodeUsage_TreatsFarFutureOrZeroExpiryAsNoExpiry(t *testing.T) {
	body := `{"models":[
    {"name":"far","size":1,"size_vram":1,"expires_at":"2999-01-01T00:00:00Z"},
    {"name":"zero","size":1,"size_vram":1,"expires_at":"0001-01-01T00:00:00Z"},
    {"name":"none","size":1,"size_vram":1,"expires_at":""}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "expiry", Kind: compute.KindOllama, Address: ts.URL})
	u := waitUsage(t, s, "node:expiry", func(u nodeUsage) bool { return u.State == nodeUsageStateOK && len(u.Models) == 3 })
	for _, want := range []string{"far", "zero", "none"} {
		var m *nodeLoadedModel
		for i := range u.Models {
			if u.Models[i].Name == want {
				m = &u.Models[i]
			}
		}
		if m == nil {
			t.Fatalf("model %q missing: %+v", want, u.Models)
		}
		if !m.NoExpiry {
			t.Errorf("%q: no_expiry must be true", want)
		}
		if m.ExpiresAt != nil {
			t.Errorf("%q: expires_at must be nil, got %v", want, m.ExpiresAt)
		}
	}
}

// TestNodeUsage_UnreachableNodeStillReportsPeerGPU checks a closed node
// endpoint: state unreachable, a short non-empty error, an empty models
// array — and GPU figures from the bound peer still present.
func TestNodeUsage_UnreachableNodeStillReportsPeerGPU(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	base := closed.URL
	closed.Close()

	peers := &fakePSRegistry{}
	peers.payloads = map[string]*observer.StatsResponse{
		"downpeer": {GPU: []observer.GPU{{Name: "peer-gpu", UtilPct: 61.5, MemUsedBytes: 4 << 30, MemTotalBytes: 16 << 30}}},
	}

	s, _ := newUsageHarness(t, compute.Node{Name: "down", Kind: compute.KindOllama, Address: base, ObserverPeer: "downpeer"})
	s.SetPeerRegistry(peers)

	u := waitUsage(t, s, "node:down", func(u nodeUsage) bool { return u.State == nodeUsageStateUnreachable })
	if u.Error == "" || len(u.Error) > 120 {
		t.Errorf("unreachable error must be short and non-empty, got %q (len %d)", u.Error, len(u.Error))
	}
	if len(u.Models) != 0 {
		t.Errorf("unreachable node must report no models, got %+v", u.Models)
	}
	if len(u.GPU) != 1 {
		t.Fatalf("peer GPU figures must still be reported on an unreachable node, got %v", u.GPU)
	}
	g := u.GPU[0]
	if g.Name != "peer-gpu" || g.UtilPct != 61.5 || g.MemUsedBytes != int64(4<<30) || g.MemTotalBytes != int64(16<<30) {
		t.Errorf("peer GPU parsed wrong: %+v", g)
	}
}

// TestNodeUsage_Non200ReplyIsUnreachable checks a 500 from /api/ps surfaces
// as unreachable with an HTTP-status error.
func TestNodeUsage_Non200ReplyIsUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "httperr", Kind: compute.KindOllama, Address: ts.URL})
	u := waitUsage(t, s, "node:httperr", func(u nodeUsage) bool { return u.State == nodeUsageStateUnreachable })
	if u.Error == "" || len(u.Error) > 120 {
		t.Errorf("unreachable error must be short and non-empty, got %q", u.Error)
	}
	if len(u.Models) != 0 {
		t.Errorf("non-200 reply must yield no models, got %+v", u.Models)
	}
}

// TestNodeUsage_MalformedBodyIsUnreachable checks a 200 with an
// unparseable body surfaces as unreachable with an "unparseable" error.
func TestNodeUsage_MalformedBodyIsUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "badbody", Kind: compute.KindOllama, Address: ts.URL})
	u := waitUsage(t, s, "node:badbody", func(u nodeUsage) bool { return u.State == nodeUsageStateUnreachable })
	if u.Error == "" || len(u.Error) > 120 {
		t.Errorf("unreachable error must be short and non-empty, got %q", u.Error)
	}
	if len(u.Models) != 0 {
		t.Errorf("malformed body must yield no models, got %+v", u.Models)
	}
}

// TestNodeUsage_SlowNodeDoesNotBlockCapacityAPI checks the first
// /api/capacity call against a node whose handler sleeps past every probe
// timeout returns well under 500ms with state pending, and a later call
// (polled, no fixed sleeps) shows the filled/unreachable state.
func TestNodeUsage_SlowNodeDoesNotBlockCapacityAPI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "slow", Kind: compute.KindOllama, Address: ts.URL})

	rec := httptest.NewRecorder()
	start := time.Now()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if elapsed >= 500*time.Millisecond {
		t.Errorf("first capacity call took %v against a slow node; it must not block on the probe", elapsed)
	}
	u := *usageFromCapacity(t, s, "node:slow")
	if u.State != nodeUsageStatePending {
		t.Errorf("first call against a slow node must be pending, got %q", u.State)
	}

	final := waitUsage(t, s, "node:slow", func(u nodeUsage) bool { return u.State != nodeUsageStatePending })
	if final.State != nodeUsageStateUnreachable && final.State != nodeUsageStateOK {
		t.Errorf("final state must be filled or unreachable, got %q", final.State)
	}
}

// TestNodeUsage_NoNewRequestWithinTTL checks a second capacity call inside
// the cached entry's TTL makes no new request to the node.
func TestNodeUsage_NoNewRequestWithinTTL(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"cached","size":1,"size_vram":1,"expires_at":"2031-01-01T00:00:00Z"}]}`))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "cachednode", Kind: compute.KindOllama, Address: ts.URL})
	c := s.nodeUsageCache()
	c.ttl = time.Hour // well beyond the test window
	n, err := s.computeReg.Get("cachednode")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	u := c.refreshSync(context.Background(), s, *n)
	if u.State != nodeUsageStateOK || u.Models[0].Name != "cached" {
		t.Fatalf("refreshSync did not fill the entry: %+v", u)
	}
	want := atomic.LoadInt32(&hits)
	if want != 1 {
		t.Fatalf("expected exactly 1 request after refreshSync, got %d", want)
	}

	usageFromCapacity(t, s, "node:cachednode")
	usageFromCapacity(t, s, "node:cachednode")
	if got := atomic.LoadInt32(&hits); got != want {
		t.Errorf("calls within the TTL made %d extra request(s) to the node", got-want)
	}
}

// TestNodeUsage_StaleServedImmediatelyThenRefreshed checks that once the
// injected clock passes the TTL, the old value is served immediately while
// exactly one refresh runs and the new value wins once it lands.
func TestNodeUsage_StaleServedImmediatelyThenRefreshed(t *testing.T) {
	var mu sync.Mutex
	body := `{"models":[{"name":"alpha","size":1,"size_vram":1,"expires_at":"2031-01-01T00:00:00Z"}]}`
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "stalenode", Kind: compute.KindOllama, Address: ts.URL})
	c := s.nodeUsageCache()
	c.ttl = time.Minute
	var clk time.Time
	clk = time.Date(2026, 2, 13, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clk }
	n, err := s.computeReg.Get("stalenode")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	first := c.refreshSync(context.Background(), s, *n)
	if first.State != nodeUsageStateOK || first.Models[0].Name != "alpha" {
		t.Fatalf("initial refresh wrong: %+v", first)
	}

	// Node now serves a different model; move the clock past the TTL.
	mu.Lock()
	body = `{"models":[{"name":"beta","size":1,"size_vram":1,"expires_at":"2031-01-02T00:00:00Z"}]}`
	mu.Unlock()
	clk = clk.Add(2 * time.Minute)

	stale := c.get(s, *n)
	if stale.State != nodeUsageStateOK || stale.Models[0].Name != "alpha" {
		t.Fatalf("stale entry must be served immediately, got: %+v", stale)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("stale read must not probe synchronously; hits=%d", got)
	}

	// The single background refresh lands within the deadline.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&hits) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("background refresh never ran after the TTL expired")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 50; i++ {
		u := c.get(s, *n)
		if len(u.Models) > 0 && u.Models[0].Name == "beta" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refreshed value never replaced the stale one")
}

// TestNodeUsage_AtMostOneRefreshPerNodeAtATime checks rapid concurrent
// gets start exactly one background refresh per node — never two.
func TestNodeUsage_AtMostOneRefreshPerNodeAtATime(t *testing.T) {
	s := &Server{}
	s.SetCapacity(capacity.New(capacity.Options{}))
	c := s.nodeUsageCache()
	c.ttl = 0

	var gate sync.WaitGroup
	gate.Add(1)
	var mu sync.Mutex
	calls, inFlight, maxInFlight := 0, 0, 0
	c.gather = func(ctx context.Context, srv *Server, n compute.Node) nodeUsage {
		mu.Lock()
		calls++
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		gate.Wait()
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nodeUsage{State: nodeUsageStateOK, Models: []nodeLoadedModel{}, GPU: []nodeGPUUsage{}}
	}

	n := compute.Node{Name: "refnode", Kind: compute.KindOllama}
	first := c.get(s, n)
	if first.State != nodeUsageStatePending {
		t.Fatalf("first get must be the pending placeholder, got %q", first.State)
	}

	// The single refresh runs in a background goroutine; wait for it to
	// enter (it parks on the gate) before firing the burst of extra gets.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		if inFlight == 1 {
			mu.Unlock()
			break
		}
		mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("first refresh never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.get(s, n)
	c.get(s, n)

	mu.Lock()
	if calls != 1 || inFlight != 1 {
		t.Fatalf("after rapid gets expected exactly 1 in-flight refresh, got calls=%d inFlight=%d", calls, inFlight)
	}
	mu.Unlock()

	gate.Done()
	drainDeadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := inFlight == 0
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(drainDeadline) {
			t.Fatal("refresh never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("stale gets launched %d refreshes for one node; want 1", calls)
	}
	if maxInFlight != 1 {
		t.Errorf("max concurrent refreshes per node was %d; want 1", maxInFlight)
	}
}

// TestNodeUsage_GPUFromPeerRegistry checks the node's bound peer's last
// pushed stats supply the GPU figures.
func TestNodeUsage_GPUFromPeerRegistry(t *testing.T) {
	peers := &fakePSRegistry{}
	peers.payloads = map[string]*observer.StatsResponse{
		"regpeer": {GPU: []observer.GPU{{Name: "reg-gpu", UtilPct: 77, MemUsedBytes: 3 << 30, MemTotalBytes: 24 << 30}}},
	}

	s, _ := newUsageHarness(t, compute.Node{Name: "peerbound", Kind: compute.KindOpenAICompat, ObserverPeer: "regpeer"})
	s.SetPeerRegistry(peers)

	u := waitUsage(t, s, "node:peerbound", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if len(u.GPU) != 1 {
		t.Fatalf("peer registry GPU missing: %+v", u.GPU)
	}
	g := u.GPU[0]
	if g.Name != "reg-gpu" || g.UtilPct != 77 || g.MemUsedBytes != int64(3<<30) || g.MemTotalBytes != int64(24<<30) {
		t.Errorf("peer registry GPU parsed wrong: %+v", g)
	}
}

// TestNodeUsage_GPUFromMonitoringEndpoint checks the configured monitoring
// endpoint (bare host, /api/stats appended) supplies the GPU figures.
func TestNodeUsage_GPUFromMonitoringEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stats" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gpu":[{"name":"mon-gpu","util_pct":12.5,"mem_used_bytes":2147483648,"mem_total_bytes":8589934592}]}`))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "monnode", Kind: compute.KindOpenAICompat, MonitoringEndpoint: ts.URL})
	u := waitUsage(t, s, "node:monnode", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if len(u.GPU) != 1 {
		t.Fatalf("monitoring endpoint GPU missing: %+v", u.GPU)
	}
	g := u.GPU[0]
	if g.Name != "mon-gpu" || g.UtilPct != 12.5 || g.MemUsedBytes != 2147483648 || g.MemTotalBytes != 8589934592 {
		t.Errorf("monitoring endpoint GPU parsed wrong: %+v", g)
	}
}

// TestNodeUsage_GPUFromSelfObserver checks that when the node's explicit
// ObserverPeer is this daemon's own hostname, the local observer's stats
// supply the GPU figures.
func TestNodeUsage_GPUFromSelfObserver(t *testing.T) {
	s, _ := newUsageHarness(t, compute.Node{Name: "selfnode", Kind: compute.KindOpenAICompat, ObserverPeer: "host-a"})
	s.hostname = "host-a"
	s.SetObserverAPI(&fakePSObserver{stats: map[string]any{"gpu": gpuDoc("self-gpu", 88.5, 1<<30, 8<<30)}})

	u := waitUsage(t, s, "node:selfnode", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if len(u.GPU) != 1 {
		t.Fatalf("self observer GPU missing: %+v", u.GPU)
	}
	g := u.GPU[0]
	if g.Name != "self-gpu" || g.UtilPct != 88.5 || g.MemUsedBytes != int64(1<<30) || g.MemTotalBytes != int64(8<<30) {
		t.Errorf("self observer GPU parsed wrong: %+v", g)
	}
}

// TestNodeUsage_DoesNotGuessSelfObserverFromNodeName checks a node whose
// name merely equals the hostname does NOT get self-observer GPU without
// an explicit ObserverPeer — no implicit name-based guessing.
func TestNodeUsage_DoesNotGuessSelfObserverFromNodeName(t *testing.T) {
	s, _ := newUsageHarness(t, compute.Node{Name: "host-a", Kind: compute.KindOpenAICompat})
	s.hostname = "host-a"
	s.SetObserverAPI(&fakePSObserver{stats: map[string]any{"gpu": gpuDoc("should-not-appear", 99, 1<<30, 8<<30)}})

	u := waitUsage(t, s, "node:host-a", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if u.GPU == nil || len(u.GPU) != 0 {
		t.Fatalf("name-only match must not attach self-observer GPU, got: %v", u.GPU)
	}
}

// TestNodeUsage_NonOllamaNodeReportsNoModels checks an openai-compat node
// is never model-probed: models_supported false, empty models array,
// state ok, and zero requests to its endpoint.
func TestNodeUsage_NonOllamaNodeReportsNoModels(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"models":[{"name":"must-not-appear"}]}`))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "openainode", Kind: compute.KindOpenAICompat, Address: ts.URL})
	u := waitUsage(t, s, "node:openainode", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })
	if u.ModelsSupported {
		t.Error("non-ollama node: models_supported must be false")
	}
	if len(u.Models) != 0 {
		t.Errorf("non-ollama node must report no models, got %+v", u.Models)
	}
	if hits != 0 {
		t.Errorf("non-ollama node must never be probed; endpoint saw %d requests", hits)
	}
}

// TestNodeUsage_NoAddressNoObserverIsNoData checks an ollama node with no
// address and no observer data at all reports state no_data.
func TestNodeUsage_NoAddressNoObserverIsNoData(t *testing.T) {
	s, _ := newUsageHarness(t, compute.Node{Name: "ghost", Kind: compute.KindOllama})
	u := waitUsage(t, s, "node:ghost", func(u nodeUsage) bool { return u.State == nodeUsageStateNoData })
	if len(u.Models) != 0 || len(u.GPU) != 0 {
		t.Errorf("no_data node must carry no models and no GPU, got %+v", u)
	}
}

// TestNodeUsage_PoolJSONShape checks the /api/capacity body: node pools
// carry usage with models and gpu as arrays (never null) plus the existing
// limit/held/queue_depth fields; host: and llm: pools carry no usage.
func TestNodeUsage_PoolJSONShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(ts.Close)

	s, _ := newUsageHarness(t, compute.Node{Name: "shapenode", Kind: compute.KindOllama, Address: ts.URL})
	s.capacityLedger.SetLimit("host", 2)
	s.capacityLedger.SetLimit("llm:vllm", 1)
	s.inferenceReg = inference.NewRegistry()
	if err := s.inferenceReg.Add(&inference.LLM{Name: "vllm", Kind: "ollama"}); err != nil {
		t.Fatalf("Add llm: %v", err)
	}

	// Let the node's usage entry fill in first so the shape check is not
	// racing the background refresh.
	waitUsage(t, s, "node:shapenode", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })

	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Pools []map[string]any `json:"pools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, p := range body.Pools {
		if n, ok := p["name"].(string); ok {
			byName[n] = p
		}
	}

	nodePool, ok := byName["node:shapenode"]
	if !ok {
		t.Fatalf("node pool missing: %s", rec.Body.String())
	}
	usage, ok := nodePool["usage"].(map[string]any)
	if !ok {
		t.Fatal("node pool lacks a usage object")
	}
	if usage["models"] == nil || usage["gpu"] == nil {
		t.Errorf("usage models and gpu must be arrays, never null: %v", usage)
	}
	for _, key := range []string{"limit", "held", "queue_depth"} {
		if _, present := nodePool[key]; !present {
			t.Errorf("node pool lost an existing field %q", key)
		}
	}

	hostPool, ok := byName["host"]
	if !ok {
		t.Fatalf("host pool missing: %s", rec.Body.String())
	}
	if _, present := hostPool["usage"]; present {
		t.Error("host: pool must not carry usage")
	}
	if _, present := hostPool["queue_depth"]; present {
		t.Error("host: pool must not carry queue_depth")
	}
	for _, key := range []string{"limit", "held"} {
		if _, present := hostPool[key]; !present {
			t.Errorf("host pool lost an existing field %q", key)
		}
	}

	llmPool, ok := byName["llm:vllm"]
	if !ok {
		t.Fatalf("llm pool missing: %s", rec.Body.String())
	}
	if _, present := llmPool["usage"]; present {
		t.Error("llm: pool must not carry usage")
	}
}

// TestNodeUsage_PruneDropsDeletedNode checks deleting a node from the
// registry evicts its cache entry on the next capacity call.
func TestNodeUsage_PruneDropsDeletedNode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"doomed","size":1,"size_vram":1,"expires_at":"2031-01-01T00:00:00Z"}]}`))
	}))
	t.Cleanup(ts.Close)

	s, reg := newUsageHarness(t, compute.Node{Name: "doomed", Kind: compute.KindOllama, Address: ts.URL})
	waitUsage(t, s, "node:doomed", func(u nodeUsage) bool { return u.State == nodeUsageStateOK })

	c := s.nodeUsageCache()
	c.mu.Lock()
	_, hadEntry := c.entries["doomed"]
	c.mu.Unlock()
	if !hadEntry {
		t.Fatal("cache entry expected after a filled read")
	}
	if err := reg.Delete("doomed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The next capacity call prunes against the (now smaller) registry.
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	c.mu.Lock()
	_, stillThere := c.entries["doomed"]
	c.mu.Unlock()
	if stillThere {
		t.Error("cache entry for a deleted node must be pruned")
	}
}
