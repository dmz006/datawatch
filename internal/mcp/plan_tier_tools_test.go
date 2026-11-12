// MCP tool surface for plan_tier + the plan tier inflight table:
//   - llm_add / llm_update carry plan_tier + max_inflight into the REST body
//   - autonomous_config_set forwards plan_tier_max_inflight (object or
//     tier=value form) to /api/autonomous/config
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
)

func planTierWebServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	idx := strings.LastIndex(srv.URL, ":")
	p, err := strconv.Atoi(srv.URL[idx+1:])
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return &Server{webPort: p}
}

func TestMCPLLMLLMAdd_PlanTierAndMaxInflight(t *testing.T) {
	var method, path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	args := map[string]any{
		"name": "cc", "kind": "claude-code",
		"plan_tier": "max_5x", "max_inflight": "3", // tool declares WithString
	}
	r := callReq(args)
	if _, err := s.handleLLMAddMCP(context.Background(), r); err != nil {
		t.Fatalf("llm_add: %v", err)
	}
	if method != http.MethodPost || path != "/api/llms" {
		t.Fatalf("route = %s %s, want POST /api/llms", method, path)
	}
	if body["plan_tier"] != "max_5x" {
		t.Errorf("plan_tier = %v, want max_5x", body["plan_tier"])
	}
	if f, _ := body["max_inflight"].(float64); f != 3 {
		t.Errorf("max_inflight = %v, want 3", body["max_inflight"])
	}
}

func TestMCPLLUMUpdate_PlanTierOnly(t *testing.T) {
	var method, path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	r := callReq(map[string]any{"name": "cc", "plan_tier": "pro"})
	if _, err := s.handleLLMUpdateMCP(context.Background(), r); err != nil {
		t.Fatalf("llm_update: %v", err)
	}
	if method != http.MethodPut || path != "/api/llms/cc" {
		t.Fatalf("route = %s %s, want PUT /api/llms/cc", method, path)
	}
	if body["plan_tier"] != "pro" {
		t.Errorf("plan_tier = %v, want pro", body["plan_tier"])
	}
	if _, present := body["max_inflight"]; present {
		t.Errorf("max_inflight should be absent when not given, got %v", body["max_inflight"])
	}
}

func TestMCPAutonomousConfigSet_PlanTierTableObject(t *testing.T) {
	var method, path string
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	r := callReq(map[string]any{
		"plan_tier_max_inflight": `{"max_5x":2,"api":0}`,
	})
	if _, err := s.handleAutonomousConfigSet(context.Background(), r); err != nil {
		t.Fatalf("config_set: %v", err)
	}
	if method != http.MethodPut || !strings.HasSuffix(path, "config") {
		t.Fatalf("route = %s %s, want PUT …config", method, path)
	}
	table, ok := body["plan_tier_max_inflight"].(map[string]any)
	if !ok {
		t.Fatalf("plan_tier_max_inflight not forwarded as object: %v (%T)", body["plan_tier_max_inflight"], body["plan_tier_max_inflight"])
	}
	if table["max_5x"] != float64(2) || table["api"] != float64(0) {
		t.Errorf("table = %v, want max_5x:2 api:0", table)
	}
}

func TestMCPAutonomousConfigSet_PlanTierTablePairs(t *testing.T) {
	var body map[string]any
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	r := callReq(map[string]any{"plan_tier_max_inflight": "pro=1,max_20x=4"})
	if _, err := s.handleAutonomousConfigSet(context.Background(), r); err != nil {
		t.Fatalf("config_set pairs: %v", err)
	}
	table, ok := body["plan_tier_max_inflight"].(map[string]any)
	if !ok {
		t.Fatalf("plan_tier_max_inflight not an object: %v", body["plan_tier_max_inflight"])
	}
	if table["pro"] != float64(1) || table["max_20x"] != float64(4) {
		t.Errorf("table = %v, want pro:1 max_20x:4", table)
	}
}

func TestMCPAutonomousConfigGet_Proxies(t *testing.T) {
	var method, path string
	s := planTierWebServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"plan_tier_max_inflight":{"max_5x":2}}`))
	})
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if method != http.MethodGet || !strings.Contains(path, "autonomous") || !strings.Contains(path, "config") {
		t.Fatalf("route = %s %s, want GET …/autonomous/config", method, path)
	}
}

func TestMCPParseTierInflightTable_RejectsBadInput(t *testing.T) {
	if _, err := parseTierInflightTable("platinum=2"); err == nil {
		t.Fatal("unknown tier accepted, want error")
	}
	if _, err := parseTierInflightTable("pro=-1"); err == nil {
		t.Fatal("negative limit accepted, want error")
	}
	if _, err := parseTierInflightTable("gibberish"); err == nil {
		t.Fatal("gibberish accepted, want error")
	}
	got, err := parseTierInflightTable(`{"api":0,"max_5x":2}`)
	if err != nil || got["api"] != 0 || got["max_5x"] != 2 {
		t.Fatalf("valid object: %v %v", got, err)
	}
}
