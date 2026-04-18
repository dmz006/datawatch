package router

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/profile"
)

// Parse grammar tests — pure, fast.
func TestParse_AgentCommands(t *testing.T) {
	cases := []struct {
		in       string
		wantVerb string
		wantID   string
		wantProj string
		wantClus string
		wantTask string
		wantText string
	}{
		{"agent list", AgentVerbList, "", "", "", "", ""},
		{"agent show abc123", AgentVerbShow, "abc123", "", "", "", ""},
		{"agent logs abc123", AgentVerbLogs, "abc123", "", "", "", ""},
		{"agent kill abc123", AgentVerbKill, "abc123", "", "", "", ""},
		{"agent spawn my-proj testing", AgentVerbSpawn, "", "my-proj", "testing", "", ""},
		{"agent spawn my-proj testing echo hello world", AgentVerbSpawn, "", "my-proj", "testing", "echo hello world", ""},
		// Missing args → help text via Command.Text
		{"agent", "", "", "", "", "", ""},
		{"agent show", AgentVerbShow, "", "", "", "", "show requires"},
		{"agent spawn my-proj", AgentVerbSpawn, "", "", "", "", "spawn requires"},
		{"agent nonsense", "nonsense", "", "", "", "", "unknown verb"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Parse(c.in)
			if got.Type != CmdAgent {
				t.Fatalf("Type=%v want agent", got.Type)
			}
			if got.AgentVerb != c.wantVerb {
				t.Errorf("Verb=%q want %q", got.AgentVerb, c.wantVerb)
			}
			if got.AgentID != c.wantID {
				t.Errorf("ID=%q want %q", got.AgentID, c.wantID)
			}
			if got.AgentProject != c.wantProj {
				t.Errorf("Project=%q want %q", got.AgentProject, c.wantProj)
			}
			if got.AgentClusterName != c.wantClus {
				t.Errorf("Cluster=%q want %q", got.AgentClusterName, c.wantClus)
			}
			if got.AgentTask != c.wantTask {
				t.Errorf("Task=%q want %q", got.AgentTask, c.wantTask)
			}
			if c.wantText != "" && !strings.Contains(got.Text, c.wantText) {
				t.Errorf("Text=%q want contains %q", got.Text, c.wantText)
			}
		})
	}
}

// fakeAgentDriver registers as docker kind for router integration tests.
type fakeAgentDriver struct{}

func (fakeAgentDriver) Kind() string                                           { return "docker" }
func (fakeAgentDriver) Spawn(_ context.Context, a *agents.Agent) error {
	a.DriverInstance = "fake-" + a.ID
	return nil
}
func (fakeAgentDriver) Status(_ context.Context, _ *agents.Agent) (agents.State, error) {
	return agents.StateReady, nil
}
func (fakeAgentDriver) Logs(_ context.Context, _ *agents.Agent, _ int) (string, error) {
	return "line-1\nline-2\n", nil
}
func (fakeAgentDriver) Terminate(_ context.Context, _ *agents.Agent) error { return nil }

func setupAgentRouter(t *testing.T) *Router {
	t.Helper()
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
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
	m.RegisterDriver(fakeAgentDriver{})
	r.SetAgentManager(m)
	return r
}

func TestAgent_List_Empty(t *testing.T) {
	r := setupAgentRouter(t)
	resp := r.HandleTestMessage("agent list")
	if len(resp) == 0 || !strings.Contains(resp[0], "(none)") {
		t.Errorf("empty list should say (none): %v", resp)
	}
}

func TestAgent_Spawn_AndList(t *testing.T) {
	r := setupAgentRouter(t)
	resp := r.HandleTestMessage("agent spawn p c echo hi")
	if len(resp) == 0 {
		t.Fatal("no spawn response")
	}
	if !strings.Contains(resp[0], "spawned agent") {
		t.Errorf("spawn msg missing expected text: %s", resp[0])
	}
	// List now has one
	resp = r.HandleTestMessage("agent list")
	if !strings.Contains(resp[0], "1 agent") {
		t.Errorf("list should show 1 agent: %s", resp[0])
	}
}

func TestAgent_Show_NotFound(t *testing.T) {
	r := setupAgentRouter(t)
	resp := r.HandleTestMessage("agent show nope")
	if !strings.Contains(resp[0], "not found") {
		t.Errorf("expected not-found: %s", resp[0])
	}
}

func TestAgent_NoManagerWired(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	resp := r.HandleTestMessage("agent list")
	if !strings.Contains(resp[0], "not configured") {
		t.Errorf("unconfigured manager should say so: %s", resp[0])
	}
}
