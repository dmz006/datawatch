// BL407 Phase 0 (B114) — POST /api/agents/report (handleAgentReport):
// a cluster-dispatched worker's only way to authenticate its own
// completion callback, and the other half of fixing B114 (a
// cluster-dispatched PRD task's verify loop polling a session ID
// that could never resolve, even on success).
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/session"
)

// reportServerFixture extends agentServerFixture with a real
// session.Manager, so the virtual-session flip can be observed.
func reportServerFixture(t *testing.T) (*Server, *agents.Manager, *session.Manager) {
	t.Helper()
	s, m := agentServerFixture(t)
	sm, err := session.NewManager(s.hostname, t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.manager = sm
	return s, m, sm
}

// registerVirtualSession mirrors what autonomousSpawn's cluster branch
// does in cmd/datawatch/main.go — not reachable from this package, so
// the test reconstructs the same shape by hand.
func registerVirtualSession(t *testing.T, sm *session.Manager, hostname, agentID string) string {
	t.Helper()
	fullID := agents.VirtualSessionFullID(hostname, agentID)
	sess := &session.Session{
		ID:            agentID[:min(8, len(agentID))],
		FullID:        fullID,
		State:         session.StateRunning,
		Hostname:      hostname,
		BackendFamily: "agent-virtual",
		AgentID:       agentID,
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return fullID
}

func TestAgentReport_SuccessFlipsVirtualSessionToComplete(t *testing.T) {
	s, m, sm := reportServerFixture(t)
	a, err := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "echo hi",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	fullID := registerVirtualSession(t, sm, s.hostname, a.ID)

	rtok := m.GetResultTokenFor(a.ID)
	if rtok == "" {
		t.Fatal("empty result token")
	}

	body := strings.NewReader(`{"status":"ok","summary":"all done"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/report", body)
	req.Header.Set("Authorization", "Bearer "+rtok)
	rr := httptest.NewRecorder()
	s.handleAgentReport(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got, ok := sm.GetSession(fullID)
	if !ok {
		t.Fatal("virtual session vanished")
	}
	if got.State != session.StateComplete {
		t.Errorf("session state=%q want complete", got.State)
	}
	if got.LastSummaryLong != "all done" {
		t.Errorf("LastSummaryLong=%q want %q", got.LastSummaryLong, "all done")
	}

	updated := m.Get(a.ID)
	if updated.Result == nil || updated.Result.Status != "ok" {
		t.Errorf("agent Result not recorded: %+v", updated.Result)
	}
}

func TestAgentReport_FailureFlipsVirtualSessionToFailed(t *testing.T) {
	s, m, sm := reportServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "echo hi",
	})
	fullID := registerVirtualSession(t, sm, s.hostname, a.ID)
	rtok := m.GetResultTokenFor(a.ID)

	body := strings.NewReader(`{"status":"fail","summary":"task session ended: failed"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/report", body)
	req.Header.Set("Authorization", "Bearer "+rtok)
	rr := httptest.NewRecorder()
	s.handleAgentReport(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got, ok := sm.GetSession(fullID)
	if !ok || got.State != session.StateFailed {
		t.Errorf("session state=%v want failed (ok=%v)", got, ok)
	}
}

func TestAgentReport_WrongTokenRejected(t *testing.T) {
	s, m, _ := reportServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "echo hi",
	})

	body := strings.NewReader(`{"status":"ok"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/report", body)
	req.Header.Set("Authorization", "Bearer not-the-real-token")
	rr := httptest.NewRecorder()
	s.handleAgentReport(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rr.Code)
	}
	if m.Get(a.ID).Result != nil {
		t.Error("RecordResult must not fire for an unauthorized report")
	}
}

func TestAgentReport_MissingTokenRejected(t *testing.T) {
	s, _, _ := reportServerFixture(t)
	body := strings.NewReader(`{"status":"ok"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/report", body)
	rr := httptest.NewRecorder()
	s.handleAgentReport(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rr.Code)
	}
}

func TestAgentReport_WrongMethodRejected(t *testing.T) {
	s, _, _ := reportServerFixture(t)
	rr := httptest.NewRecorder()
	s.handleAgentReport(rr, httptest.NewRequest(http.MethodGet, "/api/agents/report", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405", rr.Code)
	}
}

// TestBootstrap_DeliversResultTokenAndTaskSettings is the plan's own
// required coverage: the worker's bootstrap response must carry the
// result token plus the backend/effort/model/permission_mode settings
// autonomousSpawn's cluster branch threaded through at Spawn time —
// without these the worker has no way to authenticate its own
// completion callback, or to know what to run Task with.
func TestBootstrap_DeliversResultTokenAndTaskSettings(t *testing.T) {
	s, m := agentServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "echo hi",
		Backend: "claude-code", Effort: "normal", Model: "opus",
		PermissionMode: "acceptEdits",
	})
	token := m.BootstrapTokenForTest(a.ID)
	body := map[string]string{"agent_id": a.ID, "token": token}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(string(b))))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp BootstrapResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.ResultToken == "" {
		t.Error("bootstrap response missing result_token")
	}
	if resp.Backend != "claude-code" || resp.Effort != "normal" ||
		resp.Model != "opus" || resp.PermissionMode != "acceptEdits" {
		t.Errorf("task settings not threaded through: %+v", resp)
	}
}
