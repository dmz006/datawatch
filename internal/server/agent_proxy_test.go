// F10 sprint 3 (S3.5) — agent proxy handler tests.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/gorilla/websocket"
)

// proxyDriver is a Driver whose Spawn sets ContainerAddr to a
// caller-provided host:port (the httptest backend's address). Lets
// proxy tests aim the parent at a fake "worker" without needing
// a real container.
type proxyDriver struct{ addr string }

func (p *proxyDriver) Kind() string { return "docker" }
func (p *proxyDriver) Spawn(_ context.Context, a *agents.Agent) error {
	a.DriverInstance = "fake-" + a.ID
	a.ContainerAddr = p.addr
	return nil
}
func (p *proxyDriver) Status(_ context.Context, _ *agents.Agent) (agents.State, error) {
	return agents.StateReady, nil
}
func (p *proxyDriver) Logs(_ context.Context, _ *agents.Agent, _ int) (string, error) {
	return "", nil
}
func (p *proxyDriver) Terminate(_ context.Context, _ *agents.Agent) error { return nil }

// proxyFixture spins up a fake worker behind httptest and returns the
// configured server + the spawned agent's ID for routing.
func proxyFixture(t *testing.T, workerHandler http.Handler) (*Server, string, *httptest.Server) {
	t.Helper()
	worker := httptest.NewServer(workerHandler)
	t.Cleanup(worker.Close)

	u, err := url.Parse(worker.URL)
	if err != nil {
		t.Fatal(err)
	}
	addr := u.Host // host:port — no scheme, matches Agent.ContainerAddr

	dir := t.TempDir()
	ps, err := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{
		Name: "c", Kind: profile.ClusterDocker, Context: "x",
	})
	m := agents.NewManager(ps, cs)
	m.RegisterDriver(&proxyDriver{addr: addr})
	a, err := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	s := &Server{hostname: "h"}
	s.SetAgentManager(m)
	return s, a.ID, worker
}

// HTTP GET round-trip through the agent proxy.
func TestAgentProxy_HTTP_Forwarded(t *testing.T) {
	gotPath := ""
	gotMethod := ""
	gotHeader := ""
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Test")
		w.Header().Set("X-Worker-Echo", "yes")
		w.WriteHeader(202)
		_, _ = w.Write([]byte("from-worker"))
	})
	s, agentID, _ := proxyFixture(t, worker)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/agent/"+agentID+"/api/info?x=1", nil)
	req.Header.Set("X-Test", "hello")
	s.handleProxy(rr, req)

	if rr.Code != 202 {
		t.Errorf("status=%d want 202; body=%s", rr.Code, rr.Body.String())
	}
	if gotPath != "/api/info" {
		t.Errorf("worker saw path=%q want /api/info", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("worker saw method=%q want GET", gotMethod)
	}
	if gotHeader != "hello" {
		t.Errorf("worker saw X-Test=%q want hello", gotHeader)
	}
	if rr.Header().Get("X-Worker-Echo") != "yes" {
		t.Errorf("response did not propagate X-Worker-Echo: %v", rr.Header())
	}
	if !strings.Contains(rr.Body.String(), "from-worker") {
		t.Errorf("body=%q want contains from-worker", rr.Body.String())
	}
}

func TestAgentProxy_UnknownAgent_404(t *testing.T) {
	s, _, _ := proxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/agent/nonexistent/api/info", nil)
	s.handleProxy(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestAgentProxy_NoContainerAddr_503(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	// Driver intentionally leaves ContainerAddr empty (worker not yet scheduled).
	driver := &proxyDriver{addr: ""}
	m := agents.NewManager(ps, cs)
	m.RegisterDriver(driver)
	a, err := m.Spawn(context.Background(), agents.SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{hostname: "h"}
	s.SetAgentManager(m)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/agent/"+a.ID+"/api/info", nil)
	s.handleProxy(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestAgentProxy_NoManager_503(t *testing.T) {
	s := &Server{hostname: "h"} // no SetAgentManager
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/agent/abc/x", nil)
	s.handleProxy(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

// /api/proxy/agent/<id> with no trailing path → resolves to "/" on
// the worker (matches the F16 server-proxy default-path behaviour).
func TestAgentProxy_RootPath(t *testing.T) {
	gotPath := ""
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	s, agentID, _ := proxyFixture(t, worker)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/agent/"+agentID, nil)
	s.handleProxy(rr, req)
	if gotPath != "/" {
		t.Errorf("worker saw path=%q want /", gotPath)
	}
}

// WebSocket upgrade through the agent proxy: client connects to
// /api/proxy/agent/<id>/ws, parent dials worker's /ws, and bytes flow
// in both directions.
func TestAgentProxy_WebSocket_Bidirectional(t *testing.T) {
	upgr := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgr.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Echo with prefix so we can tell it's the worker, not the client.
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(websocket.TextMessage, []byte("echo:"+string(msg))); err != nil {
				return
			}
		}
	})
	s, agentID, _ := proxyFixture(t, worker)

	// Wrap parent's handleProxy in an httptest server so we have a
	// real net.Listener to dial from a websocket client.
	parent := httptest.NewServer(http.HandlerFunc(s.handleProxy))
	defer parent.Close()

	parentURL, _ := url.Parse(parent.URL)
	wsURL := "ws://" + parentURL.Host + "/api/proxy/agent/" + agentID + "/ws"
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer clientConn.Close()

	if err := clientConn.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, got, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "echo:hi" {
		t.Errorf("got %q want echo:hi", string(got))
	}
}

