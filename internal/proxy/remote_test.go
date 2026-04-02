package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestNewRemoteDispatcher(t *testing.T) {
	d := NewRemoteDispatcher(nil)
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
	if d.HasServers() {
		t.Error("empty dispatcher should have no servers")
	}
}

func TestHasServers(t *testing.T) {
	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "prod", URL: "http://localhost:9999", Enabled: false},
	})
	if d.HasServers() {
		t.Error("disabled server should not count")
	}

	d2 := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "prod", URL: "http://localhost:9999", Enabled: true},
	})
	if !d2.HasServers() {
		t.Error("enabled server should count")
	}
}

func TestFetchSessions(t *testing.T) {
	// Mock remote server
	sessions := []*session.Session{
		{ID: "a1b2", FullID: "remote-a1b2", Hostname: "remote", Task: "test task"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/sessions" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sessions)
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer ts.Close()

	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "test", URL: ts.URL, Enabled: true},
	})

	result := d.fetchSessions(d.servers[0])
	if len(result) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result))
	}
	if result[0].ID != "a1b2" {
		t.Errorf("expected a1b2, got %s", result[0].ID)
	}
}

func TestFindSession(t *testing.T) {
	sessions := []*session.Session{
		{ID: "x1y2", FullID: "remote-x1y2", Hostname: "remote"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	}))
	defer ts.Close()

	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "prod", URL: ts.URL, Enabled: true},
	})

	// Should find by short ID
	srv := d.FindSession("x1y2")
	if srv != "prod" {
		t.Errorf("expected prod, got %q", srv)
	}

	// Should find by full ID
	srv = d.FindSession("remote-x1y2")
	if srv != "prod" {
		t.Errorf("expected prod for full ID, got %q", srv)
	}

	// Unknown session
	srv = d.FindSession("zzzz")
	if srv != "" {
		t.Errorf("expected empty for unknown, got %q", srv)
	}
}

func TestForwardCommand(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/test/message" {
			var req struct{ Text string }
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"responses": []string{"[remote] response to: " + req.Text},
				"count":     1,
			})
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer ts.Close()

	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "test", URL: ts.URL, Enabled: true},
	})

	responses, err := d.ForwardCommand("test", "help")
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0] != "[remote] response to: help" {
		t.Errorf("unexpected response: %s", responses[0])
	}

	// Unknown server
	_, err = d.ForwardCommand("nonexistent", "help")
	if err == nil {
		t.Error("expected error for unknown server")
	}
}

func TestListAllSessions(t *testing.T) {
	sessions1 := []*session.Session{{ID: "a1", FullID: "s1-a1"}}
	sessions2 := []*session.Session{{ID: "b2", FullID: "s2-b2"}, {ID: "c3", FullID: "s2-c3"}}

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(sessions1)
	}))
	defer ts1.Close()
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(sessions2)
	}))
	defer ts2.Close()

	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "s1", URL: ts1.URL, Enabled: true},
		{Name: "s2", URL: ts2.URL, Enabled: true},
	})

	all := d.ListAllSessions()
	if len(all["s1"]) != 1 {
		t.Errorf("s1: expected 1 session, got %d", len(all["s1"]))
	}
	if len(all["s2"]) != 2 {
		t.Errorf("s2: expected 2 sessions, got %d", len(all["s2"]))
	}
}

func TestAuthToken(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]*session.Session{})
	}))
	defer ts.Close()

	d := NewRemoteDispatcher([]config.RemoteServerConfig{
		{Name: "auth", URL: ts.URL, Token: "secret-token", Enabled: true},
	})

	d.fetchSessions(d.servers[0])
	if gotAuth != "Bearer secret-token" {
		t.Errorf("expected Bearer secret-token, got %q", gotAuth)
	}
}
