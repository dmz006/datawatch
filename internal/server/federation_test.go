// Issue #3 — federation fan-out tests.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func federationFixture(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		manager: mgr,
		cfg:     &config.Config{},
	}
}

func TestFederation_NoRemotes_PrimaryOnly(t *testing.T) {
	s := federationFixture(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa01", FullID: "testhost-aa01", State: session.StateRunning,
		UpdatedAt: time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/federation/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got FederationResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Primary) != 1 {
		t.Errorf("primary=%d want 1", len(got.Primary))
	}
	if len(got.Proxied) != 0 {
		t.Errorf("proxied=%v want empty", got.Proxied)
	}
}

func TestFederation_RemoteOK(t *testing.T) {
	// Fake peer datawatch exposing /api/sessions.
	peerSessions := []session.Session{
		{ID: "bb01", FullID: "peer-bb01", State: session.StateRunning, UpdatedAt: time.Now()},
	}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(peerSessions)
	}))
	defer peer.Close()

	s := federationFixture(t)
	s.cfg.Servers = []config.RemoteServerConfig{
		{Name: "peer", URL: peer.URL, Enabled: true},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/federation/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got FederationResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Proxied["peer"]) != 1 {
		t.Errorf("proxied[peer]=%d want 1", len(got.Proxied["peer"]))
	}
	if len(got.Errors) != 0 {
		t.Errorf("errors=%v want empty", got.Errors)
	}
}

func TestFederation_RemoteAuthFailed_RecordedInErrors(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer peer.Close()

	s := federationFixture(t)
	s.cfg.Servers = []config.RemoteServerConfig{
		{Name: "badauth", URL: peer.URL, Enabled: true},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/federation/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got FederationResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Errors["badauth"] != "auth_failed" {
		t.Errorf("errors[badauth]=%q want auth_failed", got.Errors["badauth"])
	}
}

func TestFederation_SinceFilter(t *testing.T) {
	s := federationFixture(t)
	now := time.Now()
	_ = s.manager.SaveSession(&session.Session{
		ID: "old", FullID: "h-old", State: session.StateRunning,
		UpdatedAt: now.Add(-10 * time.Minute),
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "new", FullID: "h-new", State: session.StateRunning,
		UpdatedAt: now,
	})

	since := now.Add(-1 * time.Minute).UnixMilli()
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/federation/sessions?since=%d", since), nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	var got FederationResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Primary) != 1 || got.Primary[0].ID != "new" {
		t.Errorf("since filter broken: %+v", got.Primary)
	}
}

func TestFederation_StateFilter(t *testing.T) {
	s := federationFixture(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "run", FullID: "h-run", State: session.StateRunning, UpdatedAt: time.Now(),
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "dead", FullID: "h-dead", State: session.StateKilled, UpdatedAt: time.Now(),
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/federation/sessions?states=running", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	var got FederationResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Primary) != 1 || got.Primary[0].ID != "run" {
		t.Errorf("state filter broken: %+v", got.Primary)
	}
}

func TestFederation_RejectsPost(t *testing.T) {
	s := federationFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/federation/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}

func TestParseStateFilter(t *testing.T) {
	if got := parseStateFilter(""); got != nil {
		t.Errorf("empty → nil, got %v", got)
	}
	got := parseStateFilter("running, waiting_input")
	if !got["running"] || !got["waiting_input"] {
		t.Errorf("csv parse broken: %v", got)
	}
}
