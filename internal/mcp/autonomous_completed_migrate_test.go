// MCP surface tests for autonomous_completed_migrate: it proxies one POST to
// the completed-migration endpoint carrying dry_run, limit and id only when
// given, returns the daemon's report, and turns a refusal into a tool error.

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

const migrateReport = `{"dry_run":true,"busy":false,"considered":2,"changed":0,"would_complete":[{"id":"aaaa0001","kind":"automaton","from":"cancelled","to":"completed"}],"already_completed":0}`

func TestAutonomousCompletedMigrateMCP_ProxiesAndReturnsReport(t *testing.T) {
	var method, path string
	var q url.Values
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, q = r.Method, r.URL.Path, r.URL.Query()
		_, _ = w.Write([]byte(migrateReport))
	})
	res, err := s.handleAutonomousCompletedMigrate(context.Background(), callReq(map[string]any{
		"dry_run": true, "limit": float64(3), "id": "aaaa0001",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/completed/migrate" {
		t.Fatalf("proxied %s %s", method, path)
	}
	if q.Get("dry_run") != "true" || q.Get("limit") != "3" || q.Get("id") != "aaaa0001" {
		t.Errorf("query = %v", q)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"would_complete"`) || !strings.Contains(out, "aaaa0001") {
		t.Errorf("tool result = %q; want the daemon's report unchanged", out)
	}
}

func TestAutonomousCompletedMigrateMCP_DefaultsSendNothing(t *testing.T) {
	var raw string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"changed":1}`))
	})
	res, err := s.handleAutonomousCompletedMigrate(context.Background(), callReq(map[string]any{}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if raw != "" {
		t.Errorf("absent parameters must send no query, got %q", raw)
	}
}

func TestAutonomousCompletedMigrateMCP_RefusalIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "completed migration: a final gate is running; try again when it has finished", http.StatusConflict)
	})
	res, err := s.handleAutonomousCompletedMigrate(context.Background(), callReq(map[string]any{}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 409 must come back as a tool error: err=%v res=%+v", err, res)
	}
}

func TestAutonomousCompletedMigrateMCP_ToolRegisteredWithShapeAndCapability(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousCompletedMigrate()
	if tool.Name != "autonomous_completed_migrate" {
		t.Fatalf("name = %q", tool.Name)
	}
	for _, p := range []string{"dry_run", "limit", "id"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("missing parameter %q", p)
		}
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("no parameter may be required, got %v", tool.InputSchema.Required)
	}
	for _, w := range []string{"Automata", "Completed", "Never kills a session"} {
		if !strings.Contains(tool.Description, w) {
			t.Errorf("description lacks %q: %s", w, tool.Description)
		}
	}
	if got := federation.MCPToolCap["autonomous_completed_migrate"]; got != federation.CapAutonomousWrite {
		t.Errorf("capability = %q, want %q", got, federation.CapAutonomousWrite)
	}
}
