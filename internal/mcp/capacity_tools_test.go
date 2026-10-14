package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func capacityTestServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}
}

func callReq(args map[string]any) mcpsdk.CallToolRequest {
	r := mcpsdk.CallToolRequest{}
	r.Params.Arguments = args
	return r
}

func TestCapacityTools_Names(t *testing.T) {
	s := &Server{}
	if s.toolCapacityStatus().Name != "capacity_status" || s.toolAutonomousPRDSetPriority().Name != "autonomous_prd_set_priority" {
		t.Fatal("unexpected tool names")
	}
}

func TestCapacityStatus_ProxiesGET(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"pools":[],"leases":[],"waiting":[]}`))
	})
	res, err := s.handleCapacityStatus(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if path != "/api/capacity" || method != http.MethodGet {
		t.Fatalf("proxied %s %s", method, path)
	}
}

func TestSetPriority_ProxiesPOSTWithBody(t *testing.T) {
	var path string
	var got map[string]int
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousPRDSetPriority(context.Background(), callReq(map[string]any{"id": "p1", "priority": 4})); err != nil {
		t.Fatal(err)
	}
	if path != "/api/autonomous/prds/p1/set_priority" || got["priority"] != 4 {
		t.Fatalf("path=%s body=%v", path, got)
	}
}

func TestComputeAndLLMBodies_CarryCapacityLimits(t *testing.T) {
	nb := computeBodyFromReq(callReq(map[string]any{"name": "n", "kind": "ollama", "max_concurrent_sessions": "3"}))
	if nb["max_concurrent_sessions"] != 3 {
		t.Fatalf("node body: %v", nb)
	}
	lb := llmBodyFromReq(callReq(map[string]any{"name": "l", "kind": "ollama", "max_inflight": "2"}))
	if lb["max_inflight"] != 2 {
		t.Fatalf("llm body: %v", lb)
	}
}

func TestAutonomousConfigSet_CapacityFields(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": false, "capacity_wait_timeout_seconds": 600})); err != nil {
		t.Fatal(err)
	}
	if got["capacity_enabled"] != false || got["capacity_wait_timeout_seconds"] != float64(600) {
		t.Fatalf("body=%v", got)
	}
}

// B124 — verification_backends must be sent whenever the caller
// explicitly mentions it, including an empty array to clear a
// previously-set list. The old len(arr)>0 check dropped the key from
// the PUT body entirely for an empty array, so "clear it" was
// indistinguishable from "didn't mention it" and never took effect —
// the list kept silently overriding an explicitly configured
// verification_backend with an ask-compatible fallback candidate.
func TestAutonomousConfigSet_VerificationBackendsClearable(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})

	// Explicit empty array — must be present and empty in the body.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verification_backends": []any{}})); err != nil {
		t.Fatal(err)
	}
	arr, ok := got["verification_backends"]
	if !ok {
		t.Fatalf("verification_backends missing from body entirely for an explicit empty array: %v", got)
	}
	if s, ok := arr.([]any); !ok || len(s) != 0 {
		t.Fatalf("verification_backends = %v, want an empty array (present, length 0)", arr)
	}

	// Non-empty array still passes through unchanged.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"verification_backends": []any{"ollama"}})); err != nil {
		t.Fatal(err)
	}
	if arr, ok := got["verification_backends"].([]any); !ok || len(arr) != 1 || arr[0] != "ollama" {
		t.Fatalf("verification_backends = %v, want [\"ollama\"]", got["verification_backends"])
	}

	// Not mentioning the field at all must not add it to the body.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["verification_backends"]; ok {
		t.Fatalf("verification_backends present when the caller never mentioned it: %v", got)
	}
}

func TestAutonomousConfigSet_CIWatchFields(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"ci_watch_timeout_seconds": 1800,
		"per_prd_guardrails":       []any{"rules-review", "ci-watch"},
	})); err != nil {
		t.Fatal(err)
	}
	if got["ci_watch_timeout_seconds"] != float64(1800) {
		t.Fatalf("ci_watch_timeout_seconds not forwarded: %v", got)
	}
	if l, ok := got["per_prd_guardrails"].([]any); !ok || len(l) != 2 || l[1] != "ci-watch" {
		t.Fatalf("per_prd_guardrails not forwarded: %v", got)
	}
	// Explicit empty list clears it (presence-aware, not len>0).
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"per_prd_guardrails": []any{}})); err != nil {
		t.Fatal(err)
	}
	if l, ok := got["per_prd_guardrails"].([]any); !ok || len(l) != 0 {
		t.Fatalf("empty per_prd_guardrails must be forwarded to clear: %v", got)
	}
}

func TestAutonomousConfigSet_BlockOnFileDrift(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	for _, want := range []bool{true, false} {
		got = nil
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"block_on_file_drift": want})); err != nil {
			t.Fatal(err)
		}
		if v, ok := got["block_on_file_drift"]; !ok || v != want {
			t.Fatalf("block_on_file_drift=%v not forwarded: %v", want, got)
		}
	}
	// Not mentioning the field at all must not add it to the body.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["block_on_file_drift"]; ok {
		t.Fatalf("block_on_file_drift present when the caller never mentioned it: %v", got)
	}
}

func TestAutonomousConfigSet_MaxConcurrentTasks(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_concurrent_tasks": 3})); err != nil {
		t.Fatal(err)
	}
	if got["max_concurrent_tasks"] != float64(3) {
		t.Fatalf("max_concurrent_tasks not forwarded: %v", got)
	}
	// Values below 1 are not forwarded.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_concurrent_tasks": 0})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_concurrent_tasks"]; ok {
		t.Fatalf("max_concurrent_tasks=0 forwarded: %v", got)
	}
}

func TestAutonomousConfigSet_MaxParallelTasksDeprecatedNoteReturned(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok","deprecated_fields":{"max_parallel_tasks":"deprecated and ignored"}}`))
	})
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_parallel_tasks": 7}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_parallel_tasks"]; !ok {
		t.Fatalf("max_parallel_tasks must still be forwarded so the server can flag it: %v", got)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "deprecated_fields") {
		t.Fatalf("tool result lacks the server's deprecated_fields note: %s", text)
	}
	desc, _ := json.Marshal(s.toolAutonomousConfigSet().InputSchema.Properties["max_parallel_tasks"])
	if !strings.Contains(string(desc), "DEPRECATED") {
		t.Fatalf("max_parallel_tasks description must say DEPRECATED: %s", desc)
	}
}
