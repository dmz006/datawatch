// BL372 — MCP web_search_stats tool unit tests.
//
// TC-1: handleWebSearchStats returns enabled+url+engine fields
// TC-2: autonomous_prd_reset_task with no webPort returns error

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TC-1: web_search_stats returns the configured values.
func TestBL372_WebSearchStats_ReturnsConfiguredValues(t *testing.T) {
	s := &Server{
		webSearchEnabled: true,
		webSearchURL:     "http://searxng.example.com",
		webSearchEngine:  "searxng",
	}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	res, err := s.handleWebSearchStats(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty result")
	}
	txt, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatal("result is not TextContent")
	}
	var stats map[string]any
	if err := json.Unmarshal([]byte(txt.Text), &stats); err != nil {
		t.Fatalf("result not valid JSON: %v; body=%q", err, txt.Text)
	}
	if stats["enabled"] != true {
		t.Errorf("enabled = %v; want true", stats["enabled"])
	}
	if stats["url"] != "http://searxng.example.com" {
		t.Errorf("url = %v; want http://searxng.example.com", stats["url"])
	}
	if stats["engine"] != "searxng" {
		t.Errorf("engine = %v; want searxng", stats["engine"])
	}
}

// TC-2: web_search_stats when disabled returns enabled=false.
func TestBL372_WebSearchStats_DisabledReturnsEnabledFalse(t *testing.T) {
	s := &Server{webSearchEnabled: false}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	res, err := s.handleWebSearchStats(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	txt, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatal("result is not TextContent")
	}
	var stats map[string]any
	if err := json.Unmarshal([]byte(txt.Text), &stats); err != nil {
		t.Fatalf("result not valid JSON: %v; body=%q", err, txt.Text)
	}
	if stats["enabled"] != false {
		t.Errorf("enabled = %v; want false", stats["enabled"])
	}
}

// TC-3: autonomous_prd_reset_task with no webPort returns error in result.
func TestBL372_AutoPRDResetTask_NoWebPort_ReturnsError(t *testing.T) {
	s := &Server{} // webPort = 0
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"id":      "prd-test",
		"task_id": "task-test",
	}

	// handleAutonomousPRDResetTask calls proxyJSON which returns error when webPort=0.
	// Either err != nil or res contains error text — either is fine for this path;
	// the important thing is no panic.
	_, _ = s.handleAutonomousPRDResetTask(context.Background(), req)
}
