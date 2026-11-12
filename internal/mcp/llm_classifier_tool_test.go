package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func callLLMClassifier(t *testing.T, args map[string]any, status int) (method, path, query string, body map[string]any, res *mcpsdk.CallToolResult) {
	t.Helper()
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handleLLMClassifier(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return
}

func TestLLMClassifierToolDeclared(t *testing.T) {
	tool := (&Server{}).toolLLMClassifier()
	if tool.Name != "llm_classifier" {
		t.Fatalf("name = %q", tool.Name)
	}
	for _, k := range []string{"action", "publisher", "confirm", "automaton_id", "task_id", "low_confidence"} {
		if _, ok := tool.InputSchema.Properties[k]; !ok {
			t.Errorf("missing parameter %q", k)
		}
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "action" {
		t.Errorf("required = %v", tool.InputSchema.Required)
	}
}

func TestLLMClassifierActions(t *testing.T) {
	m, p, _, _, res := callLLMClassifier(t, map[string]any{"action": "status"}, 200)
	if m != "GET" || p != "/api/llm/classifier" || res.IsError {
		t.Fatalf("status → %s %s err=%v", m, p, res.IsError)
	}
	m, p, _, _, _ = callLLMClassifier(t, map[string]any{"action": "publishers"}, 200)
	if m != "GET" || p != "/api/llm/classifier/publishers" {
		t.Fatalf("publishers → %s %s", m, p)
	}
	m, p, _, body, _ := callLLMClassifier(t, map[string]any{"action": "add_publisher", "publisher": "qwen", "confirm": true}, 200)
	if m != "POST" || p != "/api/llm/classifier/publishers" || body["publisher"] != "qwen" || body["confirm"] != true || body["actor"] != "mcp" {
		t.Fatalf("add_publisher → %s %s %v", m, p, body)
	}
	// Without confirm the tool forwards confirm=false, so the daemon refuses.
	_, _, _, body, _ = callLLMClassifier(t, map[string]any{"action": "add_publisher", "publisher": "qwen"}, 400)
	if body["confirm"] != false {
		t.Fatalf("unconfirmed add must forward confirm=false, got %v", body)
	}
	m, p, q, _, _ := callLLMClassifier(t, map[string]any{"action": "verdicts", "automaton_id": "p1", "task_id": "t1", "low_confidence": true}, 200)
	if m != "GET" || p != "/api/llm/classifier/verdicts" || !strings.Contains(q, "automaton=p1") || !strings.Contains(q, "task=t1") || !strings.Contains(q, "low_confidence=true") {
		t.Fatalf("verdicts → %s %s ?%s", m, p, q)
	}
	_, _, q, _, _ = callLLMClassifier(t, map[string]any{"action": "verdicts", "automaton_id": "p1"}, 200)
	if strings.Contains(q, "task=") || strings.Contains(q, "low_confidence") {
		t.Fatalf("unset filters leaked into the query: %s", q)
	}
}

func TestLLMClassifierErrors(t *testing.T) {
	_, _, _, _, res := callLLMClassifier(t, map[string]any{"action": "nope"}, 200)
	if !res.IsError {
		t.Fatal("an unknown action must be an error")
	}
	_, _, _, _, res = callLLMClassifier(t, map[string]any{"action": "status"}, 500)
	if !res.IsError {
		t.Fatal("a REST failure must be an error result")
	}
}
