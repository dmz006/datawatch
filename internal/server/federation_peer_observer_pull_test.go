// Task c39a000b — observer-pull controls for the Federated Peers PWA card:
// peers added or updated through /api/federation/peers persist the
// three optional fields (observer_pull, observer_pull_interval_s,
// observer_pull_timeout_s), surface immediately in /api/observer/pull
// with their per-peer interval and timeout overrides, and switch back
// with the same endpoint.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

type pullRowForTest struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	IntervalS   int    `json:"interval_s"`
	TimeoutS    int    `json:"timeout_s"`
	Stale       bool   `json:"stale"`
	StaleReason string `json:"stale_reason"`
	Shadow      bool   `json:"shadow"`
}

func getPullRows(t *testing.T, s *Server, token string) []pullRowForTest {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/pull", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.handleObserverPull(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/observer/pull: status %d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		Total int              `json:"total"`
		Peers []pullRowForTest `json:"peers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&res); err != nil {
		t.Fatalf("decode pull response: %v", err)
	}
	return res.Peers
}

func TestFedPeer_ObserverPull_PersistAndSurface(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	adminReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer admin-token")
		return r
	}

	// Add a peer through the very endpoint the PWA card POSTs, with all
	// three observer-pull controls set — the shape the checkbox + two
	// number fields produce.
	body := map[string]any{
		"name":                     "pull-peer",
		"url":                      "http://pull-peer:8080",
		"token":                    "tok-pull-peer",
		"enabled":                  true,
		"capabilities":             []string{"federation-peer"},
		"observer_pull":            true,
		"observer_pull_interval_s": 42,
		"observer_pull_timeout_s":  7,
	}
	rb, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleFederationPeers(rr, adminReq(http.MethodPost, "/api/federation/peers", rb))
	if rr.Code != http.StatusCreated {
		t.Fatalf("add peer: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var created multiserver.Entry
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !created.ObserverPull {
		t.Errorf("observer_pull = false on create, want true")
	}
	if created.ObserverPullIntervalS != 42 {
		t.Errorf("observer_pull_interval_s = %d on create, want 42", created.ObserverPullIntervalS)
	}
	if created.ObserverPullTimeoutS != 7 {
		t.Errorf("observer_pull_timeout_s = %d on create, want 7", created.ObserverPullTimeoutS)
	}

	// The peer must surface in the pull status with its per-peer
	// overrides (42s interval, 7s timeout) and a clean pull state.
	rows := getPullRows(t, s, "admin-token")
	var mine *pullRowForTest
	for i := range rows {
		if rows[i].Name == "pull-peer" {
			mine = &rows[i]
		}
	}
	if mine == nil {
		t.Fatalf("pull-peer not in /api/observer/pull rows: %v", rows)
	}
	if mine.Source != "pull" {
		t.Errorf("source = %q, want pull", mine.Source)
	}
	if mine.IntervalS != 42 {
		t.Errorf("interval_s = %d, want the per-peer 42 (not the server default)", mine.IntervalS)
	}
	if mine.TimeoutS != 7 {
		t.Errorf("timeout_s = %d, want the per-peer 7 (not the server default)", mine.TimeoutS)
	}
	if !mine.Stale || mine.StaleReason != "collector not running" {
		t.Errorf("stale = %v reason = %q, want true/collector not running (no collector)", mine.Stale, mine.StaleReason)
	}

	// The PWA's unchecked-box path: PUT observer_pull:false. The partial
	// merge must clear the flag and the peer drops out of the pull set.
	offBody, _ := json.Marshal(map[string]any{"observer_pull": false})
	rr2 := httptest.NewRecorder()
	s.handleFederationPeers(rr2, adminReq(http.MethodPut, "/api/federation/peers/pull-peer", offBody))
	if rr2.Code != http.StatusOK {
		t.Fatalf("PUT off: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	if got := getPullRows(t, s, "admin-token"); len(got) != 0 {
		t.Fatalf("pull-peer should leave the pull set after the flag clears, got rows: %v", got)
	}

	// Round-trip back on; this time the blank number fields in the form
	// parse to 0, which the PWA transmits explicitly so the previously
	// saved per-peer overrides (42s / 7s) clear back to server defaults.
	onBody, _ := json.Marshal(map[string]any{
		"observer_pull":            true,
		"observer_pull_interval_s": 0,
		"observer_pull_timeout_s":  0,
	})
	rr3 := httptest.NewRecorder()
	s.handleFederationPeers(rr3, adminReq(http.MethodPut, "/api/federation/peers/pull-peer", onBody))
	if rr3.Code != http.StatusOK {
		t.Fatalf("PUT on: expected 200, got %d body=%s", rr3.Code, rr3.Body.String())
	}
	storeGot, ok := s.serverStore.Get("pull-peer")
	if !ok {
		t.Fatalf("store get: pull-peer not found")
	}
	store := storeGot
	if !store.ObserverPull {
		t.Errorf("observer_pull = false after re-enable, want true")
	}
	if store.ObserverPullIntervalS != 0 || store.ObserverPullTimeoutS != 0 {
		t.Errorf("interval/timeout = (%d, %d), want (0, 0) — unset means server default",
			store.ObserverPullIntervalS, store.ObserverPullTimeoutS)
	}
}

func TestFedPeer_ObserverPull_ShadowFlag(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	adminReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer admin-token")
		return r
	}

	body := map[string]any{
		"name":          "both",
		"url":           "http://both:8080",
		"token":         "tok-both",
		"enabled":       true,
		"capabilities":  []string{"federation-peer"},
		"observer_pull": true,
	}
	rb, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleFederationPeers(rr, adminReq(http.MethodPost, "/api/federation/peers", rb))
	if rr.Code != http.StatusCreated {
		t.Fatalf("add peer: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The peer also pushes stats under its own name — the pull row must
	// flag it as shadow so the PWA badge reads "shadow", not "pull".
	reg := newFakePeerRegistry()
	if _, err := reg.Register("both", "B", "v9.9", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.peerRegistry = reg

	rows := getPullRows(t, s, "admin-token")
	if len(rows) != 1 {
		t.Fatalf("want exactly one row, got %d (%v)", len(rows), rows)
	}
	if rows[0].Name != "both" || rows[0].Source != "pull" {
		t.Errorf("row = %+v, want name=both source=pull", rows[0])
	}
	if !rows[0].Shadow {
		t.Errorf("shadow = false, want true (peer also pushes under its own name)")
	}
}
