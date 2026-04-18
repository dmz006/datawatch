// F10 sprint 3.6 — comm-channel bind handler tests.

package router

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/session"
)

// setupBindRouter wires a Router with both a real session.Manager
// (pre-populated with one session) and an agent.Manager with one
// spawned agent. Used by handleBind tests.
func setupBindRouter(t *testing.T) (*Router, *session.Session, *agents.Agent) {
	t.Helper()
	dir := t.TempDir()

	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{
		ID:       "aa11",
		FullID:   "h-aa11",
		Task:     "test",
		State:    session.StateRunning,
		Hostname: "h",
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	am := agents.NewManager(ps, cs)
	am.RegisterDriver(fakeAgentDriver{})
	a, err := am.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
	})
	if err != nil {
		t.Fatal(err)
	}

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
		manager:  sm,
	}
	r.SetAgentManager(am)
	return r, sess, a
}

func TestBind_HappyPath(t *testing.T) {
	r, sess, a := setupBindRouter(t)
	resp := r.HandleTestMessage("bind h-aa11 " + a.ID)
	if len(resp) == 0 || !strings.Contains(resp[0], "bound to agent") {
		t.Fatalf("unexpected response: %v", resp)
	}
	got, _ := r.manager.GetSession(sess.FullID)
	if got.AgentID != a.ID {
		t.Errorf("AgentID=%q want %q", got.AgentID, a.ID)
	}
	ag := r.agentMgr.Get(a.ID)
	if len(ag.SessionIDs) != 1 || ag.SessionIDs[0] != sess.FullID {
		t.Errorf("agent SessionIDs=%v want [%s]", ag.SessionIDs, sess.FullID)
	}
}

func TestBind_Unbind(t *testing.T) {
	r, sess, a := setupBindRouter(t)
	_ = r.manager.SetAgentBinding(sess.FullID, a.ID)

	resp := r.HandleTestMessage("bind h-aa11 -")
	if len(resp) == 0 || !strings.Contains(resp[0], "unbound") {
		t.Fatalf("unexpected response: %v", resp)
	}
	got, _ := r.manager.GetSession(sess.FullID)
	if got.AgentID != "" {
		t.Errorf("AgentID after unbind=%q want empty", got.AgentID)
	}
}

func TestBind_UnknownSession(t *testing.T) {
	r, _, a := setupBindRouter(t)
	resp := r.HandleTestMessage("bind h-zzzz " + a.ID)
	if len(resp) == 0 || !strings.Contains(resp[0], "not found") {
		t.Errorf("expected not-found: %v", resp)
	}
}

func TestBind_UnknownAgent(t *testing.T) {
	r, _, _ := setupBindRouter(t)
	resp := r.HandleTestMessage("bind h-aa11 no-such-agent")
	if len(resp) == 0 || !strings.Contains(resp[0], "Agent") || !strings.Contains(resp[0], "not found") {
		t.Errorf("expected agent-not-found: %v", resp)
	}
}

// Note: the "empty SessionID" guard in handleBind is defence-in-depth
// only — the parser (Parse) trims whitespace so "bind " becomes
// "bind" which falls through to implicit-send, never reaching
// handleBind with an empty ID. The equivalent REST 400 branch is
// covered in internal/server/session_bind_test.go
// (TestSessionBind_MissingID_400).
