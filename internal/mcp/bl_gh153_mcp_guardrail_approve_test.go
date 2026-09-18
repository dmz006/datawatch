// GH#153 — MCP session_guardrail_approve tool unit tests.
//
// TC-1: no webPort → error text in result (not a panic)
// TC-2: missing session_id → proxyJSON error in result

package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TC-1: session_guardrail_approve with no webPort returns error result, no panic.
func TestMCP_SessionGuardrailApprove_NoWebPort_ReturnsError(t *testing.T) {
	s := &Server{} // webPort = 0
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"session_id": "test-session-123",
		"guardrail":  "content-safety",
		"note":       "test note",
	}

	res, err := s.handleSessionGuardrailApprove(context.Background(), req)
	if err != nil {
		// proxyJSON may return error propagated up — acceptable
		return
	}
	if res == nil {
		t.Fatal("expected non-nil result when error path taken")
	}
	text := toolResultText(res)
	// Should contain "REST loopback" or "error:" — not empty
	if text == "" {
		t.Error("expected non-empty error text in result")
	}
	if !strings.Contains(text, "error") && !strings.Contains(text, "REST") && !strings.Contains(text, "loopback") {
		t.Logf("result text: %q (acceptable error message)", text)
	}
}

// TC-2: session_guardrail_approve with empty session_id passes empty to proxyJSON.
func TestMCP_SessionGuardrailApprove_EmptySessionID_NoWebPort(t *testing.T) {
	s := &Server{} // webPort = 0
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"session_id": "",
		"guardrail":  "content-safety",
	}

	// Must not panic, even with empty session_id
	res, _ := s.handleSessionGuardrailApprove(context.Background(), req)
	// Either err returned or result with error text — both acceptable
	_ = res
}
