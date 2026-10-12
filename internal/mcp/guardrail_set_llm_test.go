// set_llm / set_task_llm guardrail_backend / guardrail_model forwarding.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func captureSetLLMBody(t *testing.T, call func(s *Server) error) map[string]string {
	t.Helper()
	var got map[string]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("body not JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := call(s); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return got
}

func TestMCPSetLLM_LegacyBodyUnchanged(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetLLM(context.Background(), callReq(map[string]any{"id": "p1", "backend": "opencode"}))
		return err
	})
	want := map[string]string{"backend": "opencode", "effort": "", "model": "", "decomposition_profile": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetLLM_GuardrailOnlyOmitsLegacy(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetLLM(context.Background(), callReq(map[string]any{"id": "p1", "guardrail_backend": "ollama", "guardrail_model": ""}))
		return err
	})
	want := map[string]string{"guardrail_backend": "ollama", "guardrail_model": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetLLM_GuardrailWithLegacy(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetLLM(context.Background(), callReq(map[string]any{"id": "p1", "backend": "opencode", "guardrail_model": "m1"}))
		return err
	})
	want := map[string]string{"backend": "opencode", "effort": "", "model": "", "decomposition_profile": "", "guardrail_model": "m1", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetTaskLLM_LegacyBodyUnchanged(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetTaskLLM(context.Background(), callReq(map[string]any{"id": "p1", "task_id": "t1", "backend": "opencode"}))
		return err
	})
	want := map[string]string{"task_id": "t1", "backend": "opencode", "effort": "", "model": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetTaskLLM_GuardrailOnlyOmitsLegacy(t *testing.T) {
	got := captureSetLLMBody(t, func(s *Server) error {
		_, err := s.handleAutonomousPRDSetTaskLLM(context.Background(), callReq(map[string]any{"id": "p1", "task_id": "t1", "guardrail_backend": "ollama"}))
		return err
	})
	want := map[string]string{"task_id": "t1", "guardrail_backend": "ollama", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestMCPSetLLM_GuardrailParamsDeclared(t *testing.T) {
	s := &Server{}
	for _, tool := range []string{"set_llm", "set_task_llm"} {
		tl := s.toolAutonomousPRDSetLLM()
		if tool == "set_task_llm" {
			tl = s.toolAutonomousPRDSetTaskLLM()
		}
		for _, p := range []string{"guardrail_backend", "guardrail_model"} {
			if _, ok := tl.InputSchema.Properties[p]; !ok {
				t.Errorf("%s: missing param %s", tool, p)
			}
		}
	}
	if _, ok := s.toolAutonomousPRDSetStoryLLM().InputSchema.Properties["guardrail_backend"]; ok {
		t.Error("set_story_llm must not expose guardrail_backend")
	}
}
