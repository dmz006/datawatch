package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/config"
)

func bootstrapFor(t *testing.T, s *Server, m *agents.Manager, req agents.SpawnRequest) BootstrapResponse {
	t.Helper()
	a, err := m.Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	b, _ := json.Marshal(map[string]string{"agent_id": a.ID, "token": m.BootstrapTokenForTest(a.ID)})
	rr := httptest.NewRecorder()
	s.handleAgentBootstrap(rr, httptest.NewRequest(http.MethodPost, "/api/agents/bootstrap", strings.NewReader(string(b))))
	if rr.Code != http.StatusOK {
		t.Fatalf("bootstrap %d %s", rr.Code, rr.Body.String())
	}
	var resp BootstrapResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// The worker is told the parent's push settings, with a spawn's own override
// taking the policy's place, and the profile's branch it must not push.
func TestBootstrap_DeliversPushSettings(t *testing.T) {
	s, m := agentServerFixture(t)
	s.cfg = config.DefaultConfig()
	s.cfg.Git.PushOnComplete = "never"
	s.cfg.Git.SecretScan.Mode = "enforce"
	req := agents.SpawnRequest{ProjectProfile: "p", ClusterProfile: "c", Task: "t", Branch: "automaton/one"}

	got := bootstrapFor(t, s, m, req).Git.Push
	if got == nil || got.Policy != "never" || got.SecretScanMode != "enforce" {
		t.Fatalf("push settings %+v", got)
	}
	req.Branch, req.PushOnComplete = "automaton/two", "branch"
	if got = bootstrapFor(t, s, m, req).Git.Push; got == nil || got.Policy != "branch" {
		t.Fatalf("override %+v", got)
	}
}

func TestBootstrap_UnknownSettingsSendNone(t *testing.T) {
	s, m := agentServerFixture(t) // no config wired
	resp := bootstrapFor(t, s, m, agents.SpawnRequest{ProjectProfile: "p", ClusterProfile: "c", Task: "t", Branch: "automaton/x"})
	if resp.Git.Push != nil {
		t.Fatalf("push settings without a config: %+v", resp.Git.Push)
	}
}
