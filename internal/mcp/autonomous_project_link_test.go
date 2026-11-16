package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestProjectLinkToolsNamesAndCapabilities(t *testing.T) {
	s := &Server{}
	if s.toolAutonomousPRDSetProject().Name != "autonomous_prd_set_project" || s.toolAutonomousProjectLinkBackfill().Name != "autonomous_project_link_backfill" {
		t.Fatal("unexpected tool names")
	}
	for _, name := range []string{"autonomous_prd_set_project", "autonomous_project_link_backfill"} {
		c, ok := federation.RequiredCapForMCPTool(name)
		if !ok || c != federation.CapAutonomousWrite {
			t.Errorf("%s capability = %q ok=%v, want autonomous write", name, c, ok)
		}
	}
}

func TestProjectLinkToolsAreRegistered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tl := range tools {
		seen[tl.Name] = true
	}
	for _, name := range []string{"autonomous_prd_set_project", "autonomous_project_link_backfill"} {
		if !seen[name] {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestProjectLinkToolsProxyBodies(t *testing.T) {
	var path, method string
	var body map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousPRDSetProject(context.Background(), callReq(map[string]any{"id": "p1", "project": "datawatch"})); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/p1/set_project" || body["project"] != "datawatch" {
		t.Fatalf("set_project: %s %s %v", method, path, body)
	}
	if _, err := s.handleAutonomousProjectLinkBackfill(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if path != "/api/autonomous/project-link-backfill" || body["apply"] != false {
		t.Fatalf("default must be a dry run: %s %v", path, body)
	}
	_, _ = s.handleAutonomousProjectLinkBackfill(context.Background(), callReq(map[string]any{"apply": true}))
	if body["apply"] != true {
		t.Fatalf("apply not sent: %v", body)
	}
}
