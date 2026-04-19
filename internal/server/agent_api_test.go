package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/profile"
)

// fakeSpawnDriver is a Driver stand-in used inside the server tests.
// Mirrors the internal/agents test helper — duplicated here so we
// don't create a public export purely for testing.
type fakeSpawnDriver struct {
	kind string
}

func (f *fakeSpawnDriver) Kind() string                                         { return f.kind }
func (f *fakeSpawnDriver) Spawn(_ context.Context, a *agents.Agent) error {
	a.DriverInstance = "fake-" + a.ID
	return nil
}
func (f *fakeSpawnDriver) Status(_ context.Context, _ *agents.Agent) (agents.State, error) {
	return agents.StateReady, nil
}
func (f *fakeSpawnDriver) Logs(_ context.Context, _ *agents.Agent, _ int) (string, error) {
	return "stub\n", nil
}
func (f *fakeSpawnDriver) Terminate(_ context.Context, _ *agents.Agent) error { return nil }

// agentServerFixture wires a server with real profile stores + a fake
// docker driver registered.
func agentServerFixture(t *testing.T) (*Server, *agents.Manager) {
	t.Helper()
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
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{
		Name: "c", Kind: profile.ClusterDocker, Context: "x",
	})
	m := agents.NewManager(ps, cs)
	m.RegisterDriver(&fakeSpawnDriver{kind: "docker"})
	s := &Server{hostname: "h"}
	s.SetAgentManager(m)
	return s, m
}

// ── /api/agents collection ────────────────────────────────────────────

func TestAgents_SpawnAndList(t *testing.T) {
	s, _ := agentServerFixture(t)

	body := strings.NewReader(`{"project_profile":"p","cluster_profile":"c","task":"echo"}`)
	rr := httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodPost, "/api/agents", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agents.Agent
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Errorf("agent ID empty in response")
	}
	// BootstrapToken must not be emitted in list/get responses
	if created.BootstrapToken != "" {
		t.Errorf("bootstrap token leaked in spawn response")
	}

	// List returns it
	rr = httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var list struct {
		Agents []agents.Agent `json:"agents"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&list)
	if len(list.Agents) != 1 || list.Agents[0].ID != created.ID {
		t.Errorf("list=%v want [%s]", list.Agents, created.ID)
	}
}

func TestAgents_SpawnUnknownProfile_404(t *testing.T) {
	s, _ := agentServerFixture(t)
	body := strings.NewReader(`{"project_profile":"nope","cluster_profile":"c"}`)
	rr := httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodPost, "/api/agents", body))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestAgents_SpawnMalformedJSON_400(t *testing.T) {
	s, _ := agentServerFixture(t)
	rr := httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader("{bad")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestAgents_Get_Logs_Terminate(t *testing.T) {
	s, m := agentServerFixture(t)
	a, err := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
	})
	if err != nil {
		t.Fatal(err)
	}

	// GET /api/agents/{id}
	rr := httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodGet, "/api/agents/"+a.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}

	// GET /api/agents/{id}/logs
	rr = httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodGet, "/api/agents/"+a.ID+"/logs?lines=5", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "stub\n" {
		t.Errorf("logs body=%q want stub\\n", rr.Body.String())
	}

	// DELETE /api/agents/{id}
	rr = httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodDelete, "/api/agents/"+a.ID, nil))
	if rr.Code != http.StatusNoContent {
		t.Errorf("delete status=%d", rr.Code)
	}

	// DELETE again → 404
	rr = httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodDelete, "/api/agents/"+a.ID+"fake", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("delete unknown status=%d want 404", rr.Code)
	}
}

func TestAgents_NoManager_503(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleAgents(rr, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

// ── /api/agents/bootstrap (unauthenticated) ───────────────────────────

func TestBootstrap_HappyPath(t *testing.T) {
	s, m := agentServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "echo hi",
	})

	// Reach into the manager to grab the real (in-memory) token
	// — the spawn response deliberately redacts it.
	token := m.BootstrapTokenForTest(a.ID)
	if token == "" {
		t.Fatal("empty token from manager")
	}

	body := map[string]string{"agent_id": a.ID, "token": token}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(string(b))))
	if rr.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp BootstrapResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.AgentID != a.ID {
		t.Errorf("agent_id=%q want %q", resp.AgentID, a.ID)
	}
	if resp.Task != "echo hi" {
		t.Errorf("task=%q want echo hi", resp.Task)
	}
	if resp.Env["DATAWATCH_AGENT_ID"] != a.ID {
		t.Errorf("env missing DATAWATCH_AGENT_ID")
	}

	// Second call fails (token burned)
	rr = httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(string(b))))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("reuse status=%d want 401", rr.Code)
	}
}

// F10 S6.2 — when the project profile selects a memory federation
// mode, bootstrap response carries the mode + namespace.
func TestBootstrap_DeliversMemoryBundle(t *testing.T) {
	s, m := agentServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
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

	// agentServerFixture's profile uses MemorySyncBack — so the
	// bundle should be present and namespace should derive from
	// the profile name (EffectiveNamespace = "project-p").
	if resp.Memory.Mode != "sync-back" {
		t.Errorf("Memory.Mode=%q want sync-back", resp.Memory.Mode)
	}
	if resp.Memory.Namespace != "project-p" {
		t.Errorf("Memory.Namespace=%q want project-p", resp.Memory.Namespace)
	}
}

// F10 S7.7 — when project profile lists CommInheritance, the
// bootstrap response carries the channel list verbatim.
func TestBootstrap_DeliversCommBundle(t *testing.T) {
	s, m := agentServerFixture(t)
	// Mutate the existing profile to add CommInheritance.
	prof, _ := m.GetProjectStore().Get("p")
	prof.CommInheritance = []string{"signal", "telegram"}
	if err := m.GetProjectStore().Update(prof); err != nil {
		t.Fatal(err)
	}

	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
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
	if len(resp.Comm.Channels) != 2 || resp.Comm.Channels[0] != "signal" {
		t.Errorf("Comm.Channels=%v want [signal telegram]", resp.Comm.Channels)
	}
}

func TestBootstrap_BadToken_401(t *testing.T) {
	s, m := agentServerFixture(t)
	a, _ := m.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c",
	})
	body := `{"agent_id":"` + a.ID + `","token":"wrong"}`
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(body)))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("bad-token status=%d want 401", rr.Code)
	}
}

func TestBootstrap_MissingFields_400(t *testing.T) {
	s, _ := agentServerFixture(t)
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(`{}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty body status=%d want 400", rr.Code)
	}
}

func TestBootstrap_NoManager_503(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost,
		"/api/agents/bootstrap", strings.NewReader(`{"agent_id":"x","token":"y"}`)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestBootstrap_WrongMethod_405(t *testing.T) {
	s, _ := agentServerFixture(t)
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodGet,
		"/api/agents/bootstrap", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}

// F10 S4.3 — /api/agents/ca.pem serves the configured TLS cert.
func TestAgentCAPEM_ServesPEM(t *testing.T) {
	s, _ := agentServerFixture(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")
	if err := os.WriteFile(certPath,
		[]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
		0644); err != nil {
		t.Fatal(err)
	}
	s.cfg = &config.Config{}
	s.cfg.Server.TLSEnabled = true
	s.cfg.Server.TLSCert = certPath

	rr := httptest.NewRecorder()
	s.handleAgentCAPEM(rr, httptest.NewRequest(http.MethodGet, "/api/agents/ca.pem", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "BEGIN CERTIFICATE") {
		t.Errorf("body lacks PEM marker: %q", rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/x-pem-file" {
		t.Errorf("Content-Type=%q want application/x-pem-file", got)
	}
}

func TestAgentCAPEM_TLSDisabled_404(t *testing.T) {
	s, _ := agentServerFixture(t)
	s.cfg = &config.Config{} // TLSEnabled=false
	rr := httptest.NewRecorder()
	s.handleAgentCAPEM(rr, httptest.NewRequest(http.MethodGet, "/api/agents/ca.pem", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestAgentCAPEM_WrongMethod_405(t *testing.T) {
	s, _ := agentServerFixture(t)
	s.cfg = &config.Config{}
	s.cfg.Server.TLSEnabled = true
	s.cfg.Server.TLSCert = "/dev/null"
	rr := httptest.NewRecorder()
	s.handleAgentCAPEM(rr, httptest.NewRequest(http.MethodPost, "/api/agents/ca.pem", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}
