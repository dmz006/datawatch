// Operator control MCP tools (automation_pause, automation_resume,
// automation_cancel, automation_skip_member, automation_insert_member,
// automation_remove_member, automation_reorder_edges, automation_approve_insert)
// proxied against an httptest backend.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type controlCase struct {
	tool     string
	handler  func(*Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	def      func(*Server) mcpsdk.Tool
	args     map[string]any
	wantPath string
	wantBody map[string]any
}

func controlCases() []controlCase {
	base := func(extra map[string]any) map[string]any {
		args := map[string]any{"automation_id": "ab12", "expected_version": float64(3), "actor": "alice", "reason": "because"}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}
	return []controlCase{
		{"automation_pause", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationPause
		}, (*Server).toolAutomationPause, base(nil), "/api/automations/ab12/pause",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because"}},
		{"automation_resume", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationResume
		}, (*Server).toolAutomationResume, base(nil), "/api/automations/ab12/resume",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because"}},
		{"automation_cancel", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationCancel
		}, (*Server).toolAutomationCancel, base(map[string]any{"cancel_in_flight": true}), "/api/automations/ab12/cancel",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "cancel_in_flight": true}},
		{"automation_skip_member", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationSkipMember
		}, (*Server).toolAutomationSkipMember, base(map[string]any{"member_id": "m1", "edge": "on_failed"}), "/api/automations/ab12/skip_member",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "member_id": "m1", "edge": "on_failed"}},
		{"automation_insert_member", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationInsertMember
		}, (*Server).toolAutomationInsertMember, base(map[string]any{"prd_id": "p9", "name": "late", "urgent": true}), "/api/automations/ab12/insert_member",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "urgent": true,
				"member": map[string]any{"prd_id": "p9", "name": "late"}}},
		{"automation_remove_member", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationRemoveMember
		}, (*Server).toolAutomationRemoveMember, base(map[string]any{"member_id": "m2"}), "/api/automations/ab12/remove_member",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "member_id": "m2"}},
		{"automation_reorder_edges", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationReorderEdges
		}, (*Server).toolAutomationReorderEdges, base(map[string]any{"member_id": "m1", "on_complete": "m3", "on_failed": "m2"}), "/api/automations/ab12/reorder_edges",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "member_id": "m1",
				"on_complete": "m3", "on_blocked": "", "on_failed": "m2"}},
		{"automation_approve_insert", func(s *Server) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationApproveInsert
		}, (*Server).toolAutomationApproveInsert, base(map[string]any{"member_id": "pend1"}), "/api/automations/ab12/approve_insert",
			map[string]any{"expected_version": float64(3), "actor": "alice", "reason": "because", "member_id": "pend1"}},
	}
}

func TestAutomationMCPControl(t *testing.T) {
	for _, tc := range controlCases() {
		t.Run(tc.tool, func(t *testing.T) {
			if got := tc.def(&Server{}).Name; got != tc.tool {
				t.Fatalf("tool name = %q, want %q", got, tc.tool)
			}
			s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"ok"}`)
			res, err := tc.handler(s)(context.Background(), callReq(tc.args))
			if err != nil || res == nil {
				t.Fatalf("err=%v res=%v", err, res)
			}
			if rec.method != http.MethodPost || rec.path != tc.wantPath {
				t.Errorf("proxied %s %s, want POST %s", rec.method, rec.path, tc.wantPath)
			}
			if len(rec.body) != len(tc.wantBody) {
				t.Errorf("body = %v, want %v", rec.body, tc.wantBody)
			}
			for k, want := range tc.wantBody {
				got := rec.body[k]
				if m, ok := want.(map[string]any); ok {
					gm, _ := got.(map[string]any)
					if len(gm) != len(m) {
						t.Errorf("body[%s] = %v, want %v", k, got, want)
					}
					for mk, mv := range m {
						if gm[mk] != mv {
							t.Errorf("body[%s][%s] = %v, want %v", k, mk, gm[mk], mv)
						}
					}
					continue
				}
				if got != want {
					t.Errorf("body[%s] = %v, want %v", k, got, want)
				}
			}
			if text := automationText(t, res); !strings.Contains(text, `"ok"`) {
				t.Errorf("result = %s", text)
			}
		})
	}
}

func TestAutomationMCPControlGuards(t *testing.T) {
	s, rec := activateServer(t, http.StatusConflict, "version mismatch: expected version 3, current version is 4")
	// A stale version comes back as an error result.
	res, err := s.handleAutomationPause(context.Background(), callReq(map[string]any{"automation_id": "ab12", "expected_version": float64(3)}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "version mismatch") {
		t.Errorf("conflict not surfaced: %s", text)
	}
	if _, ok := rec.body["actor"]; ok {
		t.Errorf("empty actor should not be sent: %v", rec.body)
	}

	// Calls missing a required argument never reach the server.
	rec.method = ""
	for name, call := range map[string]func() (*mcpsdk.CallToolResult, error){
		"no automation_id": func() (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationResume(context.Background(), callReq(map[string]any{"expected_version": float64(1)}))
		},
		"no expected_version": func() (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationResume(context.Background(), callReq(map[string]any{"automation_id": "ab12"}))
		},
		"insert without reason": func() (*mcpsdk.CallToolResult, error) {
			return s.handleAutomationInsertMember(context.Background(), callReq(map[string]any{
				"automation_id": "ab12", "expected_version": float64(1), "prd_id": "p9"}))
		},
	} {
		res, err := call()
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") {
			t.Errorf("%s: want an error result, got %s", name, text)
		}
	}
	if rec.method != "" {
		t.Errorf("a rejected call reached the server: %s %s", rec.method, rec.path)
	}
}

func TestAutomationMCPCancel(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"cancelled"}`)

	// Missing, null and non-boolean choices are rejected by the handler with a
	// message naming both values, and nothing is sent.
	for name, args := range map[string]map[string]any{
		"missing":     {"automation_id": "ab12", "expected_version": float64(3)},
		"null":        {"automation_id": "ab12", "expected_version": float64(3), "cancel_in_flight": nil},
		"not boolean": {"automation_id": "ab12", "expected_version": float64(3), "cancel_in_flight": "yes"},
	} {
		res, err := s.handleAutomationCancel(context.Background(), callReq(args))
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		text := automationText(t, res)
		if !strings.HasPrefix(text, "error:") || !strings.Contains(text, "cancel_in_flight=true") || !strings.Contains(text, "cancel_in_flight=false") {
			t.Errorf("%s: rejection does not name both values: %s", name, text)
		}
	}
	if rec.method != "" {
		t.Fatalf("a rejected cancel reached the server: %s %s", rec.method, rec.path)
	}

	// The schema marks the choice as required.
	var required bool
	for _, r := range (&Server{}).toolAutomationCancel().InputSchema.Required {
		if r == "cancel_in_flight" {
			required = true
		}
	}
	if !required {
		t.Errorf("cancel_in_flight is not required in the tool schema")
	}

	// Both explicit values are accepted, false included.
	for _, choice := range []bool{true, false} {
		res, err := s.handleAutomationCancel(context.Background(), callReq(map[string]any{
			"automation_id": "ab12", "expected_version": float64(3), "cancel_in_flight": choice,
		}))
		if err != nil || res == nil {
			t.Fatalf("choice %v: err=%v res=%v", choice, err, res)
		}
		if text := automationText(t, res); strings.HasPrefix(text, "error:") {
			t.Fatalf("choice %v rejected: %s", choice, text)
		}
		if rec.path != "/api/automations/ab12/cancel" || rec.body["cancel_in_flight"] != choice {
			t.Errorf("choice %v: proxied %s with body %v", choice, rec.path, rec.body)
		}
	}
}
