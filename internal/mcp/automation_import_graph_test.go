package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationImportGraphMCP(t *testing.T) {
	if got := (&Server{}).toolAutomationImportGraph().Name; got != "automation_import_graph" {
		t.Fatalf("tool name = %q", got)
	}

	// Live import.
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"draft"}`)
	res, err := s.handleAutomationImportGraph(context.Background(), callReq(map[string]any{"graph_id": "g1"}))
	if text := resultText(t, res, err); !strings.Contains(text, `"ab12"`) {
		t.Errorf("live result = %q", text)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/import-graph/g1" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["dry_run"] != false {
		t.Errorf("body = %v", rec.body)
	}

	// Dry run.
	s, rec = activateServer(t, http.StatusOK, `{"input":{"name":"g"},"dry_run":true}`)
	res, err = s.handleAutomationImportGraph(context.Background(), callReq(map[string]any{"graph_id": "g1", "dry_run": true}))
	if text := resultText(t, res, err); !strings.Contains(text, `"dry_run":true`) {
		t.Errorf("dry run result = %q", text)
	}
	if rec.body["dry_run"] != true {
		t.Errorf("body = %v", rec.body)
	}

	// Refusal text reaches the caller.
	s, _ = activateServer(t, http.StatusUnprocessableEntity, "graph not representable as an automation: the graph has 2 disconnected components")
	res, err = s.handleAutomationImportGraph(context.Background(), callReq(map[string]any{"graph_id": "g1"}))
	text := resultText(t, res, err)
	if !strings.HasPrefix(text, "error: ") || !strings.Contains(text, "disconnected components") {
		t.Errorf("refusal text = %q", text)
	}

	// Missing graph_id is refused without a request.
	res, err = s.handleAutomationImportGraph(context.Background(), callReq(map[string]any{}))
	if text := resultText(t, res, err); !strings.Contains(text, "graph_id") {
		t.Errorf("missing id text = %q", text)
	}
}
