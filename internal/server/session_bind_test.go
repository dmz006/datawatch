// F10 sprint 3.6 — session-on-worker binding tests.

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/session"
)

// bindFixture wires a Server with a real session.Manager, an agent
// manager whose Driver sets ContainerAddr on Spawn, and a spawned
// agent ready for binding. Returns server, session (persisted), agent.
func bindFixture(t *testing.T, workerHandler http.Handler) (*Server, *session.Session, *agents.Agent, *httptest.Server) {
	t.Helper()

	worker := httptest.NewServer(workerHandler)
	t.Cleanup(worker.Close)
	u, _ := url.Parse(worker.URL)

	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Pre-populate a local session directly in the store.
	sess := &session.Session{
		ID:       "aa11",
		FullID:   "h-aa11",
		Task:     "test",
		State:    session.StateRunning,
		Hostname: "h",
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Agent manager + driver that parks ContainerAddr at the fake worker.
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	am := agents.NewManager(ps, cs)
	am.RegisterDriver(&proxyDriver{addr: u.Host})
	a, err := am.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
	})
	if err != nil {
		t.Fatalf("agent Spawn: %v", err)
	}

	s := &Server{hostname: "h"}
	s.manager = sm
	s.SetAgentManager(am)
	return s, sess, a, worker
}

// POST /api/sessions/bind persists AgentID + marks the agent as
// session-bound, and returns 200 with the applied agent_id.
func TestSessionBind_Roundtrip(t *testing.T) {
	s, sess, a, _ := bindFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"id":"h-aa11","agent_id":"` + a.ID + `"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/bind", body)
	s.handleBindSessionAgent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, ok := s.manager.GetSession(sess.FullID)
	if !ok {
		t.Fatal("session vanished")
	}
	if got.AgentID != a.ID {
		t.Errorf("session.AgentID=%q want %q", got.AgentID, a.ID)
	}
	// Agent should now list the session in its SessionIDs slice.
	if updated := s.agentMgr.Get(a.ID); updated == nil {
		t.Fatal("agent vanished")
	} else {
		found := false
		for _, id := range updated.SessionIDs {
			if id == sess.FullID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agent.SessionIDs missing %q, got %v", sess.FullID, updated.SessionIDs)
		}
	}
}

// Empty agent_id unbinds an already-bound session.
func TestSessionBind_Unbind(t *testing.T) {
	s, sess, a, _ := bindFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	// Pre-bind.
	_ = s.manager.SetAgentBinding(sess.FullID, a.ID)

	body := strings.NewReader(`{"id":"h-aa11","agent_id":""}`)
	rr := httptest.NewRecorder()
	s.handleBindSessionAgent(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/bind", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	got, _ := s.manager.GetSession(sess.FullID)
	if got.AgentID != "" {
		t.Errorf("AgentID after unbind=%q want empty", got.AgentID)
	}
}

// Unknown agent → 404 (and no session mutation).
func TestSessionBind_UnknownAgent_404(t *testing.T) {
	s, sess, _, _ := bindFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	body := strings.NewReader(`{"id":"h-aa11","agent_id":"nope"}`)
	rr := httptest.NewRecorder()
	s.handleBindSessionAgent(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/bind", body))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
	got, _ := s.manager.GetSession(sess.FullID)
	if got.AgentID != "" {
		t.Errorf("AgentID should not have changed on error: got %q", got.AgentID)
	}
}

// Unknown session → 404.
func TestSessionBind_UnknownSession_404(t *testing.T) {
	s, _, a, _ := bindFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	body := strings.NewReader(`{"id":"h-zzzz","agent_id":"` + a.ID + `"}`)
	rr := httptest.NewRecorder()
	s.handleBindSessionAgent(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/bind", body))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

// Missing id → 400.
func TestSessionBind_MissingID_400(t *testing.T) {
	s, _, a, _ := bindFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	body := strings.NewReader(`{"agent_id":"` + a.ID + `"}`)
	rr := httptest.NewRecorder()
	s.handleBindSessionAgent(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/bind", body))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

// After binding, GET /api/output?id=<session> forwards to the worker
// and returns the worker's response — not the local log.
func TestSessionBind_OutputForwards(t *testing.T) {
	workerHit := false
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerHit = true
		if !strings.HasPrefix(r.URL.Path, "/api/output") {
			t.Errorf("worker saw path=%q want /api/output…", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "from-worker-log")
	})
	s, sess, a, _ := bindFixture(t, worker)
	_ = s.manager.SetAgentBinding(sess.FullID, a.ID)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/output?id="+sess.FullID+"&n=10", nil)
	s.handleSessionOutput(rr, req)

	if !workerHit {
		t.Errorf("worker was not hit; rr.Body=%q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "from-worker-log") {
		t.Errorf("body=%q want contains from-worker-log", rr.Body.String())
	}
}

// Without binding, GET /api/output continues to serve the local log
// (regression guard: the forward helper must be no-op when AgentID is
// empty so hosts without agent infrastructure are unaffected).
func TestSessionBind_OutputNotForwardedWhenUnbound(t *testing.T) {
	workerHit := false
	worker := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { workerHit = true })
	s, sess, _, _ := bindFixture(t, worker)
	// Deliberately do NOT bind.

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/output?id="+sess.FullID+"&n=10", nil)
	s.handleSessionOutput(rr, req)

	if workerHit {
		t.Error("worker proxy was hit for an unbound session")
	}
	// The local-path answer is 404 since there's no real log file behind
	// this test session — TailOutput returns an error. That's fine; we
	// only care the request didn't get forwarded.
}
