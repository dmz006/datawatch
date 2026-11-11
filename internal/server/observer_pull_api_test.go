// Observer pull protocol (pull mode) — REST surface tests:
// GET /api/observer/pull (live per-peer pull state) and the GET/PUT
// config surface for observer.pull.*.

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func pullRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var out struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out.Peers
}

func getPull(t *testing.T, s *Server, ctx context.Context) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/pull", nil).WithContext(ctx)
	s.handleObserverPull(rec, req)
	b, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return rec.Code, b
}

func TestHandleObserverPull_ListsPullTargetsWithLiveState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", nil) // peer1: interval 30s, timeout 5s
	reg := &testTickers{}
	op := &observerPull{s: s}
	op.col = observer.NewPullCollector(observer.PullCollectorOptions{Fetcher: op, NewTicker: reg.new})
	s.observerPull = op
	defer op.col.Stop()
	op.col.SetTargets(op.targets())
	waitFor(t, 2*time.Second, "initial fetch of peer1", func() bool {
		st, ok := op.col.Last("peer1")
		return ok && !st.LastAttemptAt.IsZero()
	})

	code, body := getPull(t, s, context.Background())
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", code, body)
	}
	rows := pullRows(t, body)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %s", len(rows), body)
	}
	r := rows[0]
	if r["name"] != "peer1" {
		t.Errorf("name = %v, want peer1", r["name"])
	}
	if r["source"] != "pull" {
		t.Errorf("source = %v, want \"pull\"", r["source"])
	}
	if r["shadow"] != false {
		t.Errorf("shadow = %v, want false (not push-registered)", r["shadow"])
	}
	if r["stale"] != false {
		t.Errorf("stale = %v, want false after a successful fetch", r["stale"])
	}
	if r["stale_reason"] != "none" {
		t.Errorf("stale_reason = %v, want \"none\"", r["stale_reason"])
	}
	ls, ok := r["last_success_at"].(string)
	if !ok || ls == "" {
		t.Errorf("last_success_at = %v, want a timestamp", r["last_success_at"])
	}
	if r["interval_s"] != float64(30) {
		t.Errorf("interval_s = %v, want 30", r["interval_s"])
	}
	if r["timeout_s"] != float64(5) {
		t.Errorf("timeout_s = %v, want 5", r["timeout_s"])
	}
	// Bearer tokens must never appear in the response.
	if strings.Contains(string(body), "token-abc") {
		t.Errorf("response body contains the bearer token: %s", body)
	}
}

func TestHandleObserverPull_FailedPeerIsStaleWithReason(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer okSrv.Close()
	forbiddenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer forbiddenSrv.Close()

	s := newObserverPullServer(t, okSrv.URL, "token-abc", nil) // peer1 -> 200
	e2 := &multiserver.Entry{
		Name:         "peer2",
		URL:          forbiddenSrv.URL,
		Token:        "token-2",
		Enabled:      true,
		Federated:    true,
		ObserverPull: true,
		Capabilities: []string{federation.CapObserversRead},
	}
	if err := s.serverStore.Add(e2); err != nil {
		t.Fatalf("Add peer2: %v", err)
	}
	reg := &testTickers{}
	op := &observerPull{s: s}
	op.col = observer.NewPullCollector(observer.PullCollectorOptions{Fetcher: op, NewTicker: reg.new})
	s.observerPull = op
	defer op.col.Stop()
	op.col.SetTargets(op.targets())
	waitFor(t, 2*time.Second, "fetch of peer1 and peer2", func() bool {
		a, okA := op.col.Last("peer1")
		b, okB := op.col.Last("peer2")
		return okA && !a.LastAttemptAt.IsZero() && okB && !b.LastAttemptAt.IsZero()
	})

	_, body := getPull(t, s, context.Background())
	rows := pullRows(t, body)
	byName := map[string]map[string]any{}
	for _, r := range rows {
		byName[r["name"].(string)] = r
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %s", len(rows), body)
	}
	if r1 := byName["peer1"]; r1["stale"] != false || r1["stale_reason"] != "none" {
		t.Errorf("peer1 stale/reason = %v/%v, want false/none", r1["stale"], r1["stale_reason"])
	}
	if r2 := byName["peer2"]; r2["stale"] != true {
		t.Errorf("peer2 stale = %v, want true", r2["stale"])
	}
	if r2 := byName["peer2"]; r2["stale_reason"] != "forbidden" {
		t.Errorf("peer2 stale_reason = %v, want forbidden", r2["stale_reason"])
	}
	if r2 := byName["peer2"]; r2["last_success_at"] != nil {
		t.Errorf("peer2 last_success_at = %v, want null", r2["last_success_at"])
	}
}

func TestHandleObserverPull_ShadowWhenAlsoPushRegistered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer srv.Close()

	s := newObserverPullServer(t, srv.URL, "token-abc", nil)
	fr := &fakePushRegistry{peers: map[string]observer.PeerEntry{
		"peer1": {Name: "peer1", Shape: "B"},
	}}
	s.peerRegistry = fr

	_, body := getPull(t, s, context.Background())
	rows := pullRows(t, body)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %s", len(rows), body)
	}
	if rows[0]["shadow"] != true {
		t.Errorf("shadow = %v, want true when the peer is also push-registered", rows[0]["shadow"])
	}
}

func TestHandleObserverPull_NoPeers_EmptyArray(t *testing.T) {
	s := newObserverPullServer(t, "http://127.0.0.1:1", "token-abc", func(e *multiserver.Entry) {
		e.ObserverPull = false // not in the target set
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/pull", nil).WithContext(context.Background())
	s.handleObserverPull(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	rows := pullRows(t, []byte(body))
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0: %s", len(rows), body)
	}
	if !strings.Contains(body, `"peers":[]`) {
		t.Errorf("body %q: peers should marshal as an empty array, not null", body)
	}
}

func TestHandleObserverPull_MethodNotAllowed(t *testing.T) {
	s := newObserverPullServer(t, "http://127.0.0.1:1", "token-abc", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/observer/pull", strings.NewReader("{}")).WithContext(context.Background())
	s.handleObserverPull(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandleObserverPull_CapabilityDenied(t *testing.T) {
	s := newObserverPullServer(t, "http://127.0.0.1:1", "token-abc", nil)
	// A federated peer that lacks observers:read must be rejected before
	// any state is exposed.
	denied := &multiserver.Entry{
		Name:         "peer1",
		Capabilities: []string{federation.CapSessionsList},
	}
	ctx := context.WithValue(context.Background(), fedPeerKey, denied)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/pull", nil).WithContext(ctx)
	s.handleObserverPull(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a peer without %s", rec.Code, federation.CapObserversRead)
	}
	// With the capability granted it passes.
	allowed := &multiserver.Entry{
		Name:         "peer1",
		Capabilities: []string{federation.CapObserversRead},
	}
	ctx2 := context.WithValue(context.Background(), fedPeerKey, allowed)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/observer/pull", nil).WithContext(ctx2)
	s.handleObserverPull(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a peer with %s", rec2.Code, federation.CapObserversRead)
	}
}

// fakePushRegistry is the minimum PeerRegistryAPI a shadow test needs.
type fakePushRegistry struct {
	peers map[string]observer.PeerEntry
}

func (f *fakePushRegistry) Register(_, _ string, _ string, _ map[string]any) (string, error) {
	return "", nil
}
func (f *fakePushRegistry) Verify(_, _ string) (*observer.PeerEntry, error) { return nil, nil }
func (f *fakePushRegistry) RecordPush(_ string, _ *observer.StatsResponse) error {
	return nil
}
func (f *fakePushRegistry) Get(name string) (observer.PeerEntry, bool) {
	e, ok := f.peers[name]
	return e, ok
}
func (f *fakePushRegistry) LastPayload(_ string) *observer.StatsResponse { return nil }
func (f *fakePushRegistry) List() []observer.PeerEntry {
	var out []observer.PeerEntry
	for _, e := range f.peers {
		out = append(out, e)
	}
	return out
}
func (f *fakePushRegistry) Delete(_ string) error { return nil }

// --- observer.pull.* config surface ---------------------------------------

func TestConfigPut_ObserverPullGlobalsRoundTrip(t *testing.T) {
	cfgPath := t.TempDir() + "/config.json"
	cfg := &config.Config{}
	cfg.Observer.Pull = config.DefaultObserverPullConfig()
	s := &Server{cfg: cfg, cfgPath: cfgPath}

	body := `{"observer.pull.enabled":true,"observer.pull.default_interval_s":20,"observer.pull.default_timeout_s":9}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)).WithContext(context.Background())
	s.handlePutConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !cfg.Observer.Pull.Enabled {
		t.Error("observer.pull.enabled = false, want true")
	}
	if cfg.Observer.Pull.DefaultIntervalS != 20 {
		t.Errorf("observer.pull.default_interval_s = %d, want 20", cfg.Observer.Pull.DefaultIntervalS)
	}
	if cfg.Observer.Pull.DefaultTimeoutS != 9 {
		t.Errorf("observer.pull.default_timeout_s = %d, want 9", cfg.Observer.Pull.DefaultTimeoutS)
	}

	// GET /api/config reflects the same values under observer.pull.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/config", nil).WithContext(context.Background())
	s.handleGetConfig(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec2.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode GET body: %v", err)
	}
	obs, ok := out["observer"].(map[string]any)
	if !ok {
		t.Fatalf("GET body has no top-level \"observer\" object: %v", out)
	}
	pull, ok := obs["pull"].(map[string]any)
	if !ok {
		t.Fatalf("observer has no \"pull\" object: %v", obs)
	}
	if pull["enabled"] != true {
		t.Errorf("GET observer.pull.enabled = %v, want true", pull["enabled"])
	}
	if pull["default_interval_s"] != float64(20) {
		t.Errorf("GET observer.pull.default_interval_s = %v, want 20", pull["default_interval_s"])
	}
	if pull["default_timeout_s"] != float64(9) {
		t.Errorf("GET observer.pull.default_timeout_s = %v, want 9", pull["default_timeout_s"])
	}
}

func TestConfigPut_ObserverPullRejectsUnderrunValues(t *testing.T) {
	cfgPath := t.TempDir() + "/config.json"
	cfg := &config.Config{}
	cfg.Observer.Pull = config.DefaultObserverPullConfig()
	s := &Server{cfg: cfg, cfgPath: cfgPath}

	// interval below the 5-second floor and a negative timeout are both
	// rejected (listed in "skipped"), the valid one is applied.
	body := `{"observer.pull.default_interval_s":2,"observer.pull.default_timeout_s":-1,"observer.pull.enabled":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)).WithContext(context.Background())
	s.handlePutConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	defaults := config.DefaultObserverPullConfig()
	if cfg.Observer.Pull.DefaultIntervalS != defaults.DefaultIntervalS {
		t.Errorf("interval = %d, want the shipping default %d (2 must be rejected)", cfg.Observer.Pull.DefaultIntervalS, defaults.DefaultIntervalS)
	}
	if cfg.Observer.Pull.DefaultTimeoutS != defaults.DefaultTimeoutS {
		t.Errorf("timeout = %d, want the shipping default %d (-1 must be rejected)", cfg.Observer.Pull.DefaultTimeoutS, defaults.DefaultTimeoutS)
	}
	if !cfg.Observer.Pull.Enabled {
		t.Error("enabled should still have been applied")
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode PUT body: %v", err)
	}
	skipped, _ := resp["skipped"].([]any)
	names := make([]string, 0, len(skipped))
	for _, k := range skipped {
		names = append(names, k.(string))
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "observer.pull.default_interval_s") || !strings.Contains(joined, "observer.pull.default_timeout_s") {
		t.Errorf("skipped = %v, want both invalid keys listed", names)
	}
}

func TestHandleObserverPull_NilCollectorIsStale(t *testing.T) {
	s := newObserverPullServer(t, "http://127.0.0.1:1", "token-abc", nil)
	s.observerPull = nil
	code, body := getPull(t, s, context.Background())
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	rows := pullRows(t, body)
	if len(rows) != 1 || rows[0]["stale"] != true || rows[0]["stale_reason"] != "collector not running" {
		t.Fatalf("rows = %s, want stale true / collector not running", body)
	}
	if info := s.pullInfoFor("peer1"); !info.Stale || info.StaleReason != "collector not running" {
		t.Errorf("pullInfoFor = %+v, want stale/collector not running", info)
	}
}

func TestHandleObserverPull_StoppedCollectorIsStale(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(statsOK))
	defer srv.Close()
	s := newObserverPullServer(t, srv.URL, "token-abc", nil)
	reg := &testTickers{}
	op := &observerPull{s: s}
	op.col = observer.NewPullCollector(observer.PullCollectorOptions{Fetcher: op, NewTicker: reg.new})
	s.observerPull = op
	op.col.SetTargets(op.targets())
	waitFor(t, 2*time.Second, "initial fetch", func() bool {
		st, ok := op.col.Last("peer1")
		return ok && !st.LastSuccessAt.IsZero()
	})
	op.col.Stop()
	_, body := getPull(t, s, context.Background())
	rows := pullRows(t, body)
	if len(rows) != 1 || rows[0]["stale"] != true || rows[0]["stale_reason"] != "collector not running" {
		t.Fatalf("rows = %s, want stale true / collector not running", body)
	}
	if info := s.pullInfoFor("peer1"); !info.Stale || info.StaleReason != "collector not running" || info.LastSuccessAt == nil {
		t.Errorf("pullInfoFor = %+v", info)
	}
}
