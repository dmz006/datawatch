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
