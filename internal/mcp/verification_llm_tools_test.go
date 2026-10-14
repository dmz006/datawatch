// autonomous_prd_set_{task_,story_,}verification_llm tool definitions
// and presence-aware body forwarding.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func captureVerificationLLM(t *testing.T, call func(s *Server) error) (string, map[string]string) {
	t.Helper()
	var path string
	var got map[string]string
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

func TestMCPVerificationLLM_ToolsDeclared(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		tool    mcpsdk.Tool
		name    string
		unitKey string
	}{
		{s.toolAutonomousPRDSetTaskVerificationLLM(), "autonomous_prd_set_task_verification_llm", "task_id"},
		{s.toolAutonomousPRDSetStoryVerificationLLM(), "autonomous_prd_set_story_verification_llm", "story_id"},
		{s.toolAutonomousPRDSetVerificationLLM(), "autonomous_prd_set_verification_llm", ""},
	} {
		if tc.tool.Name != tc.name {
			t.Errorf("name = %q, want %q", tc.tool.Name, tc.name)
		}
		params := []string{"id", "verification_backend", "verification_model", "verification_effort", "actor"}
		if tc.unitKey != "" {
			params = append(params, tc.unitKey)
		}
		for _, p := range params {
			if _, ok := tc.tool.InputSchema.Properties[p]; !ok {
				t.Errorf("%s: missing param %s", tc.name, p)
			}
		}
	}
}

func TestMCPVerificationLLM_ProxyPathAndBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handler  func(*Server, context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
		args     map[string]any
		wantPath string
		want     map[string]string
	}{
		{
			"task supplied keys only",
			(*Server).handleAutonomousPRDSetTaskVerificationLLM,
			map[string]any{"id": "p1", "task_id": "t1", "verification_backend": "ollama"},
			"POST /api/autonomous/prds/p1/set_task_verification_llm",
			map[string]string{"task_id": "t1", "verification_backend": "ollama", "actor": "operator"},
		},
		{
			"story empty string forwarded as clear",
			(*Server).handleAutonomousPRDSetStoryVerificationLLM,
			map[string]any{"id": "p1", "story_id": "s1", "verification_model": "", "verification_effort": "high", "actor": "bot"},
			"POST /api/autonomous/prds/p1/set_story_verification_llm",
			map[string]string{"story_id": "s1", "verification_model": "", "verification_effort": "high", "actor": "bot"},
		},
		{
			"automaton no override keys",
			(*Server).handleAutonomousPRDSetVerificationLLM,
			map[string]any{"id": "p1"},
			"POST /api/autonomous/prds/p1/set_verification_llm",
			map[string]string{"actor": "operator"},
		},
	} {
		path, got := captureVerificationLLM(t, func(s *Server) error {
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
