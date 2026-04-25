package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// BL172 (S11) — chat-channel parity tests for `peers …`. Uses the
// existing HandleTestMessage capture path and an httptest fake parent
// in place of the daemon's loopback so the assertions don't depend on
// a running server.

// ── Parser ────────────────────────────────────────────────────────────────

func TestParse_Peers_Bare(t *testing.T) {
	cmd := Parse("peers")
	if cmd.Type != CmdPeers {
		t.Errorf("type = %v want CmdPeers", cmd.Type)
	}
	if cmd.Text != "" {
		t.Errorf("text = %q want empty", cmd.Text)
	}
}

func TestParse_Peers_WithName(t *testing.T) {
	cmd := Parse("peers ollama-box")
	if cmd.Type != CmdPeers {
		t.Errorf("type = %v want CmdPeers", cmd.Type)
	}
	if cmd.Text != "ollama-box" {
		t.Errorf("text = %q", cmd.Text)
	}
}

func TestParse_Peers_StatsSubcommand(t *testing.T) {
	cmd := Parse("peers ollama-box stats")
	if cmd.Type != CmdPeers || cmd.Text != "ollama-box stats" {
		t.Errorf("got %+v", cmd)
	}
}

func TestParse_Peers_Register(t *testing.T) {
	cmd := Parse("peers register gpu1 C 0.1.0")
	if cmd.Type != CmdPeers || cmd.Text != "register gpu1 C 0.1.0" {
		t.Errorf("got %+v", cmd)
	}
}

func TestParse_Peers_Delete(t *testing.T) {
	cmd := Parse("peers delete gpu1")
	if cmd.Type != CmdPeers || cmd.Text != "delete gpu1" {
		t.Errorf("got %+v", cmd)
	}
}

// ── Handler (via HandleTestMessage + fake parent) ─────────────────────────

// fakeParentSrv stands in for the local datawatch loopback; the
// router's commGet/commJSON helpers hit cfg.Server.Port over plain
// HTTP, so we override the port to point at this server.
func newFakePeerParent(t *testing.T, handler http.HandlerFunc) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// Extract the port from srv.URL ("http://127.0.0.1:NNNN")
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return srv, port
}

func setupPeerRouter(t *testing.T, port int) *Router {
	t.Helper()
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

func TestPeers_List_HitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/observer/peers" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"peers":[{"name":"p1","shape":"B"}]}`))
	})
	r := setupPeerRouter(t, port)
	resp := r.HandleTestMessage("peers")
	if calls != 1 {
		t.Errorf("expected 1 call to parent, got %d", calls)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "p1") {
		t.Errorf("response did not include peer name: %v", resp)
	}
}

func TestPeers_Get_ByName(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/observer/peers/ollama-box" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"ollama-box","shape":"B"}`))
	})
	r := setupPeerRouter(t, port)
	resp := r.HandleTestMessage("peers ollama-box")
	if len(resp) == 0 || !strings.Contains(resp[0], "ollama-box") {
		t.Errorf("response missing peer: %v", resp)
	}
}

func TestPeers_Stats_Subcommand(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/observer/peers/ollama-box/stats" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"v":2,"host":{"name":"ollama-box"}}`))
	})
	r := setupPeerRouter(t, port)
	resp := r.HandleTestMessage("peers ollama-box stats")
	if len(resp) == 0 || !strings.Contains(resp[0], "ollama-box") {
		t.Errorf("response missing host name: %v", resp)
	}
}

func TestPeers_Register_PostsBody(t *testing.T) {
	captured := ""
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		captured = string(buf[:n])
		_, _ = w.Write([]byte(`{"name":"gpu1","shape":"C","token":"tok-xyz"}`))
	})
	r := setupPeerRouter(t, port)
	resp := r.HandleTestMessage("peers register gpu1 C")
	if !strings.Contains(captured, `"name":"gpu1"`) || !strings.Contains(captured, `"shape":"C"`) {
		t.Errorf("body missing fields: %q", captured)
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "tok-xyz") {
		t.Errorf("response missing token: %v", resp)
	}
}

func TestPeers_Register_RejectsMissingName(t *testing.T) {
	r := setupPeerRouter(t, 0)
	resp := r.HandleTestMessage("peers register")
	if len(resp) == 0 || !strings.Contains(strings.ToLower(resp[0]), "usage") {
		t.Errorf("missing-name should explain usage, got %v", resp)
	}
}

func TestPeers_Delete_HitsParent(t *testing.T) {
	calls := 0
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/api/observer/peers/gpu1" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r := setupPeerRouter(t, port)
	resp := r.HandleTestMessage("peers delete gpu1")
	if calls != 1 {
		t.Errorf("expected 1 DELETE, got %d", calls)
	}
	if len(resp) == 0 {
		t.Errorf("no response")
	}
}

func TestPeers_Delete_RejectsMissingName(t *testing.T) {
	r := setupPeerRouter(t, 0)
	resp := r.HandleTestMessage("peers delete")
	if len(resp) == 0 || !strings.Contains(strings.ToLower(resp[0]), "usage") {
		t.Errorf("missing-name should explain usage, got %v", resp)
	}
}
