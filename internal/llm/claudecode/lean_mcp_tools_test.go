package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// leanStartingContextBudget is the measured size (bytes) of the lean
// supervisor starting context for the fixed input below, plus a small margin.
// Growing the first-turn context past it is a regression.
const leanStartingContextBudget = 200 // measured 174 bytes

// TestLeanSupervisorLaunch_MCPToolsReachableAndContextSmall launches a lean
// supervisor profile through the fake backend and checks the written MCP
// config, the flags that make its tools reachable, and the context size.
func TestLeanSupervisorLaunch_MCPToolsReachableAndContextSmall(t *testing.T) {
	t.Setenv("TMUX", "")
	_ = os.Unsetenv("TMUX")

	p, _ := workerprofile.Resolve(workerprofile.ResolveInput{Role: "worker", Kind: workerprofile.KindSupervisor})
	if p.Name == "" {
		t.Fatal("no lean supervisor profile resolved")
	}
	w := WorkerProfileLaunch{
		Profile: p,
		MCPServers: map[string]channel.MCPServerSpec{
			channel.ProjectMCPEntryName: {Command: "fake-mcp", Args: []string{"serve"}},
		},
		Starting: autonomous.StartingContextInput{Role: "worker", TaskSpec: "supervise"},
	}

	d := t.TempDir()
	argv := startFakeTmux(t, false)
	b := backendOf()
	if err := b.LaunchWithProfile(context.Background(), w, "", "cs-lean-mcp", d, "/nonexistent/log"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	cmd := recordedCmd(t, argv)

	raw, err := os.ReadFile(launchedPath(t, cmd, "--mcp-config"))
	if err != nil {
		t.Fatalf("worker-mcp.json not written: %v", err)
	}
	var cfg struct {
		MCPServers map[string]channel.MCPServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("worker-mcp.json: %v", err)
	}
	spec, ok := cfg.MCPServers[channel.ProjectMCPEntryName]
	if !ok {
		t.Fatalf("server %q missing from worker-mcp.json: %s", channel.ProjectMCPEntryName, raw)
	}
	if spec.Command != "fake-mcp" {
		t.Errorf("server command = %q, want fake-mcp", spec.Command)
	}

	for _, want := range []string{
		"--strict-mcp-config",
		"--mcp-config",
		channel.WorkerMCPConfigFileName,
		"ENABLE_TOOL_SEARCH=true",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("launch command missing %q: %s", want, cmd)
		}
	}

	ctx, err := os.ReadFile(launchedPath(t, cmd, "--append-system-prompt"))
	if err != nil {
		t.Fatalf("starting context not written: %v", err)
	}
	t.Logf("lean starting context: %d bytes", len(ctx))
	if len(ctx) > leanStartingContextBudget {
		t.Errorf("lean starting context is %d bytes, budget %d", len(ctx), leanStartingContextBudget)
	}
}
