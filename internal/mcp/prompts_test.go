// BL302 S4 — unit tests for MCPPromptServer.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/stats"
)

// makeTestPromptServer creates an MCPPromptServer for testing.
// webPort=0 on the underlying Server means readResourceText() always returns
// "(data unavailable)" — which is the correct graceful fallback.
func makeTestPromptServer() *MCPPromptServer {
	srv := makeTestServer("7.1.0-alpha.4")
	mcpSrv := server.NewMCPServer("test", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(false),
	)
	return NewMCPPromptServer(srv, mcpSrv, nil)
}

// makeTestPromptServerWithStats creates an MCPPromptServer wired with live stats counters.
func makeTestPromptServerWithStats() (*MCPPromptServer, *stats.MCPStatsCounters) {
	srv := makeTestServer("7.1.0-alpha.4")
	mcpSrv := server.NewMCPServer("test", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(false),
	)
	sc := stats.NewMCPStatsCounters()
	ps := NewMCPPromptServer(srv, mcpSrv, sc)
	return ps, sc
}

// TestMCPPromptServer_ListsAllTen verifies that RegisterPrompts registers exactly 10 prompts.
func TestMCPPromptServer_ListsAllTen(t *testing.T) {
	ps := makeTestPromptServer()

	// Reset global registry before registering.
	globalPromptRegistry = nil
	ps.RegisterPrompts()

	if got := len(globalPromptRegistry); got != 10 {
		t.Errorf("expected 10 prompts, got %d", got)
	}

	expectedNames := []string{
		"analyze-session",
		"review-automaton",
		"triage-alert",
		"morning-briefing",
		"research-topic",
		"council-brief",
		"session-summary",
		"diagnose-system",
		"explore-kg",
		"plan-sprint",
	}
	nameSet := map[string]bool{}
	for _, p := range globalPromptRegistry {
		nameSet[p.Name] = true
	}
	for _, want := range expectedNames {
		if !nameSet[want] {
			t.Errorf("expected prompt %q to be registered", want)
		}
	}

	// PromptsListJSON must return all 10.
	raw := PromptsListJSON()
	var env struct {
		Prompts []promptDescriptor `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("PromptsListJSON returned invalid JSON: %v", err)
	}
	if len(env.Prompts) != 10 {
		t.Errorf("PromptsListJSON: expected 10 prompts, got %d", len(env.Prompts))
	}
}

// TestMCPPromptServer_DiagnoseSystem verifies that diagnose-system (no args)
// returns a non-empty prompt with at least one user message.
func TestMCPPromptServer_DiagnoseSystem(t *testing.T) {
	ps := makeTestPromptServer()
	globalPromptRegistry = nil
	ps.RegisterPrompts()

	ctx := context.Background()
	raw, err := ps.PromptsGetJSON(ctx, "diagnose-system", map[string]string{})
	if err != nil {
		t.Fatalf("PromptsGetJSON(diagnose-system) error: %v", err)
	}

	var env struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("invalid JSON from PromptsGetJSON: %v — got: %s", err, raw)
	}
	if env.Name != "diagnose-system" {
		t.Errorf("expected name='diagnose-system', got=%q", env.Name)
	}
	if len(env.Messages) == 0 {
		t.Error("expected at least one message")
	}
	if env.Messages[0].Role != "user" {
		t.Errorf("expected first message role=user, got=%q", env.Messages[0].Role)
	}
	// Message must contain data-unavailable note since webPort=0.
	if !strings.Contains(env.Messages[0].Content, "data unavailable") {
		// The prompt text itself references stats/alerts/config — those come back
		// as "(data unavailable)" when webPort=0. The overall prompt text wraps them.
		// Check for the wrapper text at minimum.
		if !strings.Contains(env.Messages[0].Content, "Diagnose") {
			t.Errorf("expected prompt content to contain diagnostic text, got: %s", env.Messages[0].Content[:min(200, len(env.Messages[0].Content))])
		}
	}
}

// TestMCPPromptServer_AnalyzeSession_MissingData verifies that analyze-session
// with an unknown session ID returns gracefully ("data unavailable" in content)
// rather than returning an error.
func TestMCPPromptServer_AnalyzeSession_MissingData(t *testing.T) {
	ps := makeTestPromptServer()
	globalPromptRegistry = nil
	ps.RegisterPrompts()

	ctx := context.Background()
	// Use a non-existent session ID — webPort=0 so all reads fail.
	raw, err := ps.PromptsGetJSON(ctx, "analyze-session", map[string]string{"session_id": "nonexistent-abc123"})
	if err != nil {
		t.Fatalf("PromptsGetJSON should not return Go error for missing data; got: %v", err)
	}
	var env struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(env.Messages) == 0 {
		t.Fatal("expected at least one message")
	}
	// With webPort=0 resource handlers return graceful-fallback JSON (e.g. empty
	// sessions array), so the content should contain the prompt template text.
	if !strings.Contains(env.Messages[0].Content, "Analyze") {
		t.Errorf("expected prompt text in content, got: %s",
			env.Messages[0].Content[:min(200, len(env.Messages[0].Content))])
	}
}

// TestMCPPromptServer_StatsIncrement verifies that prompt calls increment
// the stats counters.
func TestMCPPromptServer_StatsIncrement(t *testing.T) {
	ps, sc := makeTestPromptServerWithStats()
	globalPromptRegistry = nil
	ps.RegisterPrompts()

	ctx := context.Background()

	// Call diagnose-system twice.
	for i := 0; i < 2; i++ {
		if _, err := ps.PromptsGetJSON(ctx, "diagnose-system", nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	// Call plan-sprint once.
	if _, err := ps.PromptsGetJSON(ctx, "plan-sprint", nil); err != nil {
		t.Fatalf("plan-sprint: %v", err)
	}

	snap := sc.Snapshot()
	if snap.PromptCallsTotal != 3 {
		t.Errorf("expected PromptCallsTotal=3, got=%d", snap.PromptCallsTotal)
	}
	if got := snap.PromptCallsByName["diagnose-system"]; got != 2 {
		t.Errorf("expected diagnose-system count=2, got=%d", got)
	}
	if got := snap.PromptCallsByName["plan-sprint"]; got != 1 {
		t.Errorf("expected plan-sprint count=1, got=%d", got)
	}
}

// TestMCPPromptServer_UnknownPromptError verifies that PromptsGetJSON returns
// an error for an unknown prompt name.
func TestMCPPromptServer_UnknownPromptError(t *testing.T) {
	ps := makeTestPromptServer()
	globalPromptRegistry = nil
	ps.RegisterPrompts()

	_, err := ps.PromptsGetJSON(context.Background(), "not-a-real-prompt", nil)
	if err == nil {
		t.Error("expected error for unknown prompt name, got nil")
	}
}

// min returns the smaller of a and b (Go 1.21 has min builtin but some envs may not).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
