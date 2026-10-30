package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestOverlapReadToolProxiesRouteAndReturnsBody(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"warnings":[{"kind":"automaton","id":"prd-old","score":0.9}],"threshold":0.5,"mode":"shadow"}`))
	})
	res, err := s.handleAutonomousOverlapSearch(context.Background(), callReq(map[string]any{
		"title": "Ingest feeds", "spec": "Pull the feeds nightly.", "limit": float64(5),
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/overlap" {
		t.Errorf("proxied %s %s", method, path)
	}
	for key, want := range map[string]string{"title": "Ingest feeds", "spec": "Pull the feeds nightly.", "limit": "5"} {
		if got := query[key]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %v, want [%s]", key, got, want)
		}
	}
	text := automationText(t, res)
	for _, want := range []string{`"id":"prd-old"`, `"threshold":0.5`, `"mode":"shadow"`} {
		if !strings.Contains(text, want) {
			t.Errorf("result lost the daemon body (%s): %s", want, text)
		}
	}
}

func TestOverlapReadToolSendsOnlyGivenArguments(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"warnings":[],"threshold":0.5,"mode":"off"}`))
	})
	if _, err := s.handleAutonomousOverlapSearch(context.Background(), callReq(map[string]any{"spec": "Only a spec."})); err != nil {
		t.Fatal(err)
	}
	if len(query) != 1 || query["spec"] == nil {
		t.Errorf("query = %v, want only spec", query)
	}
}

func TestOverlapReadToolRouteErrorIsToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "title or spec is required", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousOverlapSearch(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "title or spec is required") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestOverlapReadToolSchemaAndCapability(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousOverlapSearch()
	if tool.Name != "autonomous_overlap_search" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("autonomous_overlap_search must be marked read-only")
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("every parameter is optional at the tool level, got required %v", tool.InputSchema.Required)
	}
	for _, name := range []string{"title", "spec", "limit"} {
		if _, ok := tool.InputSchema.Properties[name]; !ok {
			t.Errorf("lacks the %s parameter", name)
		}
	}
	if got := federation.MCPToolCap["autonomous_overlap_search"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_overlap_search = %q, want %q", got, federation.CapAutonomousRead)
	}
}
