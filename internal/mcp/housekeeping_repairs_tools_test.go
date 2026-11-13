package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestHousekeepingRepairsMCPToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		if tl.Name == "housekeeping_repairs" {
			found = true
			for _, w := range []string{"would", "did", "report", "skipped_unsafe"} {
				if !strings.Contains(tl.Description, w) {
					t.Errorf("description lacks %q", w)
				}
			}
		}
	}
	if !found {
		t.Fatal("housekeeping_repairs is not registered")
	}
	tool := (&Server{}).toolHousekeepingRepairs()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("must be read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("takes no arguments, schema has %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["housekeeping_repairs"]; got != federation.CapAutonomousRead {
		t.Errorf("capability = %q", got)
	}
}

func TestHousekeepingRepairsMCPProxiesGet(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"grace_seconds":120,"groups":[]}`))
	})
	res, err := s.handleHousekeepingRepairs(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/housekeeping-repairs" {
		t.Errorf("proxied %s %s", method, path)
	}
	if !strings.Contains(automationText(t, res), `"grace_seconds":120`) {
		t.Errorf("body not passed through: %s", automationText(t, res))
	}
}

func TestHousekeepingRepairsMCPRefusesArguments(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	res, err := s.handleHousekeepingRepairs(context.Background(), callReq(map[string]any{"mode": "on"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
	if called {
		t.Error("a refused call must not reach the daemon")
	}
	if !strings.Contains(automationText(t, res), "mode") {
		t.Errorf("error does not name the argument: %s", automationText(t, res))
	}
}

func TestHousekeepingRepairsMCPToolNames(t *testing.T) {
	if got := HousekeepingRepairsToolNames(); len(got) != 1 || got[0] != "housekeeping_repairs" {
		t.Errorf("names = %v", got)
	}
}
