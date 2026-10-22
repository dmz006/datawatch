// MCP tests for automation_set_llm and automation_set_verification_llm,
// proxied against an httptest backend.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type llmToolCase struct {
	name     string
	tool     func(*Server) mcpsdk.Tool
	handler  func(*Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	path     string
	settings []string
}

var llmToolCases = []llmToolCase{
	{"automation_set_llm", (*Server).toolAutomationSetLLM,
		func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationSetLLM
		}, "/api/automations/ab12/set_llm", []string{"backend", "model", "effort", "planning_backend", "planning_model"}},
	{"automation_set_verification_llm", (*Server).toolAutomationSetVerificationLLM,
		func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationSetVerificationLLM
		}, "/api/automations/ab12/set_verification_llm", []string{"backend", "model", "effort"}},
}

func (c llmToolCase) check(t *testing.T) {
	t.Helper()
	def := c.tool(&Server{})
	if def.Name != c.name {
		t.Fatalf("tool name = %q, want %q", def.Name, c.name)
	}
	if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "automation_id" {
		t.Errorf("required = %v, want only automation_id", def.InputSchema.Required)
	}
	for _, k := range append([]string{"expected_version", "actor", "reason"}, c.settings...) {
		if _, ok := def.InputSchema.Properties[k]; !ok {
			t.Errorf("schema lacks %s", k)
		}
	}
	if _, ok := def.InputSchema.Properties["planning_backend"]; ok != (c.name == "automation_set_llm") {
		t.Errorf("planning_backend in schema = %v", ok)
	}

	// Right path and body; an empty string is forwarded so it clears the field.
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"ok"}`)
	args := map[string]any{"automation_id": "ab12", "expected_version": float64(3), "actor": "alice", "reason": "because",
		"backend": "claude-code", "model": "", "effort": "high"}
	res, err := c.handler(s)(context.Background(), callReq(args))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != c.path {
		t.Errorf("proxied %s %s, want POST %s", rec.method, rec.path, c.path)
	}
	want := map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because",
		"backend": "claude-code", "model": "", "effort": "high"}
	if len(rec.body) != len(want) {
		t.Errorf("body = %v, want %v", rec.body, want)
	}
	for k, v := range want {
		if got, ok := rec.body[k]; !ok || got != v {
			t.Errorf("body[%s] = %v (present=%v), want %v", k, got, ok, v)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok"`) {
		t.Errorf("result = %s", text)
	}

	// expected_version is optional: omitted or 0 is not sent; a lone setting suffices.
	rec.method = ""
	if _, err := c.handler(s)(context.Background(), callReq(map[string]any{"automation_id": "ab12", "effort": ""})); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPost || len(rec.body) != 1 || rec.body["effort"] != "" {
		t.Errorf("body without version = %v (method %q)", rec.body, rec.method)
	}
	rec.method = ""
	_, _ = c.handler(s)(context.Background(), callReq(map[string]any{"automation_id": "ab12", "expected_version": float64(0), "model": "m"}))
	if _, ok := rec.body["expected_version"]; ok {
		t.Errorf("expected_version 0 should not be sent: %v", rec.body)
	}

	// Refused before any request.
	rec.method = ""
	for name, a := range map[string]map[string]any{
		"no automation_id": {"backend": "claude-code"},
		"no setting field": {"automation_id": "ab12", "expected_version": float64(3), "actor": "alice"},
	} {
		res, err := c.handler(s)(context.Background(), callReq(a))
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") {
			t.Errorf("%s: result = %s", name, text)
		}
		if rec.method != "" {
			t.Errorf("%s: request reached the server", name)
		}
	}

	// A server conflict comes back as an error result.
	s409, _ := activateServer(t, http.StatusConflict, "version mismatch")
	res, _ = c.handler(s409)(context.Background(), callReq(map[string]any{"automation_id": "ab12", "expected_version": float64(1), "model": "m"}))
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "version mismatch") {
		t.Errorf("conflict not surfaced: %s", text)
	}
}

func TestAutomationMCPSetLLM(t *testing.T) {
	c := llmToolCases[0]
	c.check(t)

	// The planning fields are forwarded too.
	s, rec := activateServer(t, http.StatusOK, `{}`)
	_, _ = s.handleAutomationSetLLM(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "planning_backend": "pi-prompt", "planning_model": ""}))
	if rec.body["planning_backend"] != "pi-prompt" || rec.body["planning_model"] != "" || len(rec.body) != 2 {
		t.Errorf("planning body = %v", rec.body)
	}
}

func TestAutomationMCPSetVerificationLLM(t *testing.T) {
	c := llmToolCases[1]
	c.check(t)

	// Planning arguments are not part of this tool and are never forwarded.
	s, rec := activateServer(t, http.StatusOK, `{}`)
	res, _ := s.handleAutomationSetVerificationLLM(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "planning_backend": "pi-prompt"}))
	if rec.method != "" || !strings.HasPrefix(automationText(t, res), "error:") {
		t.Errorf("planning-only call reached the server: %v", rec.body)
	}
}
