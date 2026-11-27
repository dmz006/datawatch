package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func acceptanceToolNames(t *testing.T, on bool) map[string]bool {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	tools, err := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test", AcceptanceTools: on}).ListTools()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "acceptance_") {
			got[tool.Name] = true
		}
	}
	return got
}

func TestAcceptanceTools_RegisteredOnlyInReportMode(t *testing.T) {
	if got := acceptanceToolNames(t, false); len(got) != 0 {
		t.Fatalf("off: %v", got)
	}
	got := acceptanceToolNames(t, true)
	if len(got) != 6 {
		t.Fatalf("on: %v", got)
	}
	for name := range got {
		if _, ok := federation.RequiredCapForMCPTool(name); !ok {
			t.Errorf("%s has no capability", name)
		}
		for _, bad := range []string{"verdict", "evidence", "set_met"} {
			if strings.Contains(name, bad) {
				t.Errorf("%s: no tool submits a result", name)
			}
		}
	}
}

func TestAcceptanceTools_WritesSayTheyComeFromATool(t *testing.T) {
	s := &Server{callerSessionID: "w1"}
	body := map[string]any{}
	var req mcpsdk.CallToolRequest
	req.Params.Arguments = map[string]any{"actor": "operator", "via": "cli", "via_session": "other"}
	s.acceptanceCallerBody(context.Background(), req, body)
	if body["via"] != "mcp" || body["via_session"] != "w1" || body["actor"] != "operator" {
		t.Fatalf("%v", body)
	}
}
