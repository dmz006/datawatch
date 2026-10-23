// BL463 — REST parity for get_my_scope: GET /api/sessions/self/scope.
// Same data contract as the MCP tool (both are built by
// channel.BuildSelfScope). The MCP surface additionally resolves self via
// the in-band caller-scope credential (HLLM-004); REST uses the same
// hint-then-channel-ready-fallback rule as GET /api/sessions/self, with
// resolved_via always naming which one applied.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func selfScopeServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := session.NewManager("h1", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.WaitBackground)
	mgr.WithFakeTmux()
	return &Server{hostname: "h1", manager: mgr}
}

func selfScopeSave(t *testing.T, s *Server, id, fullID string, ready bool) {
	t.Helper()
	now := time.Now()
	sess := &session.Session{
		ID: id, FullID: fullID, Hostname: "h1",
		Name: "n-" + id, Task: "t", ProjectDir: "/tmp/" + id,
		TmuxSession: "cs-h1-" + id, State: session.StateRunning,
		ChannelReady: ready,
		CreatedAt:    now, UpdatedAt: now,
	}
	if err := s.manager.SaveSession(sess); err != nil { //nolint:errcheck
		t.Fatalf("SaveSession: %v", err)
	}
}

func getSelfScope(t *testing.T, s *Server, hint string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/sessions/self/scope"
	if hint != "" {
		url += "?hint=" + hint
	}
	rr := httptest.NewRecorder()
	s.handleSessionSelfScope(rr, httptest.NewRequest(http.MethodGet, url, nil))
	return rr
}

// TestSessionSelfScope_ScopeFields is the isolation-clarity half of BL463
// over REST: the self session learns its own identity, its per-session
// MCP server name, the shared .mcp.json entry name, and every peer's
// name — and confirms its name is distinct from all of theirs.
func TestSessionSelfScope_ScopeFields(t *testing.T) {
	s := selfScopeServer(t)
	selfScopeSave(t, s, "aa01", "h1-aa01", true)
	selfScopeSave(t, s, "bb01", "h1-bb01", false)

	rr := getSelfScope(t, s, "aa01")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]any{
		"session_id":        "aa01",
		"full_id":           "h1-aa01",
		"hostname":          "h1",
		"state":             "running",
		"mode":              "host",
		"project_dir":       "/tmp/aa01",
		"tmux_session":      "cs-h1-aa01",
		"channel_ready":     true,
		"mcp_server_name":   "datawatch-h1-aa01",
		"shared_entry_name": "datawatch",
		"resolved_via":      "hint",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("field %q = %v, want %v", k, body[k], v)
		}
	}

	peers, ok := body["peers"].([]any)
	if !ok || len(peers) != 1 {
		t.Fatalf("peers = %v, want exactly the one peer bb01", body["peers"])
	}
	peer1, ok := peers[0].(map[string]any)
	if !ok {
		t.Fatalf("peer not an object: %T", peers[0])
	}
	if peer1["full_id"] != "h1-bb01" || peer1["mcp_server_name"] != "datawatch-h1-bb01" {
		t.Errorf("peer wrong: %v", peer1)
	}
}

// TestSessionSelfScope_FallbackWithoutHint mirrors handleSessionSelf's
// channel-ready rule, flagged as such in resolved_via.
func TestSessionSelfScope_FallbackWithoutHint(t *testing.T) {
	s := selfScopeServer(t)
	selfScopeSave(t, s, "aa01", "h1-aa01", true) // the channel-ready one
	selfScopeSave(t, s, "bb01", "h1-bb01", false)

	rr := getSelfScope(t, s, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["session_id"] != "aa01" || body["resolved_via"] != "channel-ready-fallback" {
		t.Errorf("got session_id=%v resolved_via=%v, want aa01 / channel-ready-fallback",
			body["session_id"], body["resolved_via"])
	}
}

func TestSessionSelfScope_HintUnknown404(t *testing.T) {
	s := selfScopeServer(t)
	selfScopeSave(t, s, "aa01", "h1-aa01", true)
	if rr := getSelfScope(t, s, "zz9z"); rr.Code != http.StatusNotFound {
		t.Errorf("unknown hint should be 404, got %d", rr.Code)
	}
}

func TestSessionSelfScope_NoSessions404(t *testing.T) {
	s := selfScopeServer(t)
	if rr := getSelfScope(t, s, ""); rr.Code != http.StatusNotFound {
		t.Errorf("no sessions should be 404, got %d", rr.Code)
	}
}

func TestSessionSelfScope_MethodNotAllowed(t *testing.T) {
	s := selfScopeServer(t)
	selfScopeSave(t, s, "aa01", "h1-aa01", true)
	rr := httptest.NewRecorder()
	s.handleSessionSelfScope(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/self/scope", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be 405, got %d", rr.Code)
	}
}
