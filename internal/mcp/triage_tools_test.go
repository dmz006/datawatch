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

func TestTriageToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	if !have["autonomous_triage"] {
		t.Error("tool \"autonomous_triage\" is not registered")
	}
}

func TestTriageToolSchema(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousTriage()
	if tool.Name != "autonomous_triage" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("autonomous_triage must be marked read-only")
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("all parameters are optional, got required %v", tool.InputSchema.Required)
	}
	for _, name := range []string{"automation_id", "prd_id", "status"} {
		if _, ok := tool.InputSchema.Properties[name]; !ok {
			t.Errorf("lacks the %s parameter", name)
		}
	}
	desc := tool.Description
	for _, word := range []string{"alert_minutes", "failed", "blocked"} {
		if !strings.Contains(desc, word) {
			t.Errorf("description does not mention %q: %s", word, desc)
		}
	}
}

func TestTriageProxiesGETAndPassesFiltersThrough(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"records":[],"alert_minutes":30}`))
	})
	res, err := s.handleAutonomousTriage(context.Background(), callReq(map[string]any{
		"automation_id": "auto-a", "prd_id": "prd-b", "status": "failed",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/triage" {
		t.Errorf("proxied %s %s", method, path)
	}
	for key, want := range map[string]string{"automation_id": "auto-a", "prd_id": "prd-b", "status": "failed"} {
		if got := query[key]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %v, want [%s]", key, got, want)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"alert_minutes":30`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestTriageSendsNoQueryWhenAbsent(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousTriage(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if len(query) != 0 {
		t.Errorf("query sent without any argument: %v", query)
	}
}

func TestTriageSurfacesValidationError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `status must be "failed" or "blocked"`, http.StatusBadRequest)
	})
	res, err := s.handleAutonomousTriage(context.Background(), callReq(map[string]any{"status": "pending"}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, `status must be "failed" or "blocked"`) {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestTriageCapabilityMapping(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_triage"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_triage = %q, want %q", got, federation.CapAutonomousRead)
	}
}
