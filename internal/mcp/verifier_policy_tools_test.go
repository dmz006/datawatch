// autonomous_prd_set_{task_,}verifier_policy tool definitions, presence-aware
// body forwarding, and the verifier_policy_* arguments of
// autonomous_config_set.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func captureVerifierPolicy(t *testing.T, call func(s *Server) error) (string, map[string]any) {
	t.Helper()
	var path string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("body not JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := call(s); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return path, got
}

func TestVerifierPolicyTools_Declared(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		tool    mcpsdk.Tool
		name    string
		unitKey string
	}{
		{s.toolAutonomousPRDSetVerifierPolicy(), "autonomous_prd_set_verifier_policy", ""},
		{s.toolAutonomousPRDSetTaskVerifierPolicy(), "autonomous_prd_set_task_verifier_policy", "task_id"},
	} {
		if tc.tool.Name != tc.name {
			t.Errorf("name = %q, want %q", tc.tool.Name, tc.name)
		}
		params := []string{"id", "mode", "backends", "actor"}
		if tc.unitKey != "" {
			params = append(params, tc.unitKey)
		}
		for _, p := range params {
			if _, ok := tc.tool.InputSchema.Properties[p]; !ok {
				t.Errorf("%s: missing param %s", tc.name, p)
			}
		}
		wantRequired := []string{"id"}
		if tc.unitKey != "" {
			wantRequired = append(wantRequired, tc.unitKey)
		}
		if !reflect.DeepEqual(tc.tool.InputSchema.Required, wantRequired) {
			t.Errorf("%s: required = %v, want %v", tc.name, tc.tool.InputSchema.Required, wantRequired)
		}
	}
}

func TestVerifierPolicyTools_CapsListed(t *testing.T) {
	for _, name := range []string{"autonomous_prd_set_verifier_policy", "autonomous_prd_set_task_verifier_policy"} {
		cap, ok := federation.MCPToolCap[name]
		if !ok || cap != federation.CapAutonomousWrite {
			t.Errorf("%s: capability = %q (listed %v), want %q", name, cap, ok, federation.CapAutonomousWrite)
		}
	}
}

func TestVerifierPolicyTools_ProxyPathAndBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handler  func(*Server, context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
		args     map[string]any
		wantPath string
		want     map[string]any
	}{
		{
			"automaton supplied keys only",
			(*Server).handleAutonomousPRDSetVerifierPolicy,
			map[string]any{"id": "p1", "mode": "prefer-free"},
			"POST /api/autonomous/prds/p1/set_verifier_policy",
			map[string]any{"mode": "prefer-free", "actor": "operator"},
		},
		{
			"task set both",
			(*Server).handleAutonomousPRDSetTaskVerifierPolicy,
			map[string]any{"id": "p1", "task_id": "t1", "mode": "balance", "backends": []any{"a", "b"}, "actor": "bot"},
			"POST /api/autonomous/prds/p1/set_task_verifier_policy",
			map[string]any{"task_id": "t1", "mode": "balance", "backends": []any{"a", "b"}, "actor": "bot"},
		},
		{
			"empty mode and empty list forwarded as clear",
			(*Server).handleAutonomousPRDSetTaskVerifierPolicy,
			map[string]any{"id": "p1", "task_id": "t1", "mode": "", "backends": []any{}},
			"POST /api/autonomous/prds/p1/set_task_verifier_policy",
			map[string]any{"task_id": "t1", "mode": "", "backends": []any{}, "actor": "operator"},
		},
		{
			"automaton no override keys",
			(*Server).handleAutonomousPRDSetVerifierPolicy,
			map[string]any{"id": "p1"},
			"POST /api/autonomous/prds/p1/set_verifier_policy",
			map[string]any{"actor": "operator"},
		},
	} {
		path, got := captureVerifierPolicy(t, func(s *Server) error {
			_, err := tc.handler(s, context.Background(), callReq(tc.args))
			return err
		})
		if path != tc.wantPath {
			t.Errorf("%s: path = %q, want %q", tc.name, path, tc.wantPath)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: body = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVerifierPolicyTools_RefusalIsToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `prd "p1" status "running" is locked`, http.StatusBadRequest)
	})
	res, err := s.handleAutonomousPRDSetVerifierPolicy(context.Background(), callReq(map[string]any{"id": "p1", "mode": "balance"}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a REST refusal must surface as a tool error, got %+v", res)
	}
}

func TestVerifierPolicyTools_ConfigSetParams(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	for _, p := range []string{
		"verifier_policy_mode", "verifier_policy_backends", "verifier_policy_claude_session_cap",
		"verifier_policy_claude_cap_window_minutes", "verifier_policy_force_stronger_on_retry",
		"verifier_policy_final_gate_stronger", "verifier_policy_stronger_backend", "verifier_policy_stronger_model",
	} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("autonomous_config_set: missing param %s", p)
		}
	}
	// The pre-existing parameters survive the option merge.
	for _, p := range []string{"enabled", "verification_backends", "default_auto_pr"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("autonomous_config_set: lost param %s", p)
		}
	}
}

func TestVerifierPolicyTools_ConfigSetBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{
			"every key",
			map[string]any{
				"verifier_policy_mode":                      "claude-only",
				"verifier_policy_backends":                  []any{"claude-code"},
				"verifier_policy_claude_session_cap":        float64(3),
				"verifier_policy_claude_cap_window_minutes": float64(45),
				"verifier_policy_force_stronger_on_retry":   true,
				"verifier_policy_final_gate_stronger":       true,
				"verifier_policy_stronger_backend":          "claude-code",
				"verifier_policy_stronger_model":            "claude-opus-5-5",
			},
			map[string]any{"verifier_policy": map[string]any{
				"mode": "claude-only", "backends": []any{"claude-code"}, "claude_session_cap": float64(3),
				"claude_cap_window_minutes": float64(45), "force_stronger_on_retry": true, "final_gate_stronger": true,
				"stronger_backend": "claude-code", "stronger_model": "claude-opus-5-5",
			}},
		},
		{
			"explicit zero, false, empty string and empty list are forwarded",
			map[string]any{
				"verifier_policy_mode":                    "",
				"verifier_policy_backends":                []any{},
				"verifier_policy_claude_session_cap":      float64(0),
				"verifier_policy_force_stronger_on_retry": false,
			},
			map[string]any{"verifier_policy": map[string]any{
				"mode": "", "backends": []any{}, "claude_session_cap": float64(0), "force_stronger_on_retry": false,
			}},
		},
		{
			"unrelated key leaves the policy out",
			map[string]any{"enabled": true},
			map[string]any{"enabled": true},
		},
	} {
		path, got := captureVerifierPolicy(t, func(s *Server) error {
			_, err := s.handleAutonomousConfigSet(context.Background(), callReq(tc.args))
			return err
		})
		if path != "PUT /api/autonomous/config" {
			t.Errorf("%s: path = %q", tc.name, path)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: body = %v, want %v", tc.name, got, tc.want)
		}
	}
}
