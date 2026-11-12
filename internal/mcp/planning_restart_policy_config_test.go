package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// planningRestartPolicyRequest is one request the fake daemon received.
type planningRestartPolicyRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// planningRestartPolicyDaemon records every request the tool sends, so a test
// can see which path carried the policy, or that nothing was sent at all.
func planningRestartPolicyDaemon(t *testing.T) (*Server, *[]planningRestartPolicyRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []planningRestartPolicyRequest
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		seen = append(seen, planningRestartPolicyRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	})
	return s, &seen
}

// Each valid value is sent on PUT /api/config under the dotted daemon key, and
// the autonomous-manager body is not sent for it.
func TestPlanningRestartPolicyConfigSet_SendsValidValueToDaemonConfig(t *testing.T) {
	for _, want := range []string{"reattach", "restart", "draft"} {
		t.Run(want, func(t *testing.T) {
			s, seen := planningRestartPolicyDaemon(t)
			res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"planning_restart_policy": want}))
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("valid value %q was refused", want)
			}
			if len(*seen) != 1 {
				t.Fatalf("want one request, got %d: %+v", len(*seen), *seen)
			}
			got := (*seen)[0]
			if got.Method != http.MethodPut || got.Path != "/api/config" {
				t.Fatalf("request = %s %s, want PUT /api/config", got.Method, got.Path)
			}
			if got.Body["autonomous.planning_restart_policy"] != want {
				t.Fatalf("body = %v, want autonomous.planning_restart_policy=%q", got.Body, want)
			}
		})
	}
}

// An invalid value is refused in the tool, and no request reaches the daemon.
func TestPlanningRestartPolicyConfigSet_RefusesInvalidBeforeSending(t *testing.T) {
	cases := []any{"bogus", "", "never", 7}
	for _, v := range cases {
		s, seen := planningRestartPolicyDaemon(t)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"planning_restart_policy": v}))
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("value %#v was accepted, want refused", v)
		}
		if len(*seen) != 0 {
			t.Fatalf("value %#v reached the daemon: %+v", v, *seen)
		}
		text := planningRestartPolicyResultText(res)
		for _, want := range []string{"reattach", "restart", "draft"} {
			if !strings.Contains(text, want) {
				t.Errorf("refusal for %#v does not name %q: %s", v, want, text)
			}
		}
	}
}

// When the caller does not mention the policy, no config request carries it.
func TestPlanningRestartPolicyConfigSet_AbsentIsNotSent(t *testing.T) {
	s, seen := planningRestartPolicyDaemon(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	for _, r := range *seen {
		if _, ok := r.Body["autonomous.planning_restart_policy"]; ok {
			t.Fatalf("policy sent though the caller never gave it: %+v", r)
		}
	}
}

// The schema names the three values, the enum, and the how-to link, and uses
// Automaton wording rather than PRD.
func TestPlanningRestartPolicyConfigSet_SchemaDeclaresEnumAndHowto(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	prop, ok := tool.InputSchema.Properties["planning_restart_policy"].(map[string]any)
	if !ok {
		t.Fatalf("autonomous_config_set has no planning_restart_policy property")
	}
	enum, _ := prop["enum"].([]string)
	want := []string{"reattach", "restart", "draft"}
	if len(enum) != len(want) {
		t.Fatalf("enum = %v, want %v", prop["enum"], want)
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Fatalf("enum = %v, want %v", enum, want)
		}
	}
	desc, _ := prop["description"].(string)
	if !strings.Contains(desc, "docs/howto/restart-with-work-running.md") {
		t.Errorf("description lacks the how-to link: %s", desc)
	}
	if strings.Contains(desc, "PRD") {
		t.Errorf("description uses PRD wording: %s", desc)
	}
}

func planningRestartPolicyResultText(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
