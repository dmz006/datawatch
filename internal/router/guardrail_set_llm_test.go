// comm `autonomous set-llm` / `set-task-llm` guardrail_backend= /
// guardrail_model= token handling.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func commSetLLMBody(t *testing.T, msg string) (path string, body map[string]string) {
	t.Helper()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	})
	r := setupPeerRouter(t, port)
	r.HandleTestMessage(msg)
	return path, body
}

func TestCommSetLLM_LegacyUnchanged(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set-llm p1 opencode high qwen3")
	want := map[string]string{"backend": "opencode", "effort": "high", "model": "qwen3", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_llm" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommSetLLM_GuardrailOnly(t *testing.T) {
	_, got := commSetLLMBody(t, "autonomous set-llm p1 guardrail_backend=ollama guardrail_model=")
	want := map[string]string{"guardrail_backend": "ollama", "guardrail_model": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCommSetLLM_GuardrailWithPositional(t *testing.T) {
	_, got := commSetLLMBody(t, "autonomous set-llm p1 opencode guardrail_model=m1 high")
	want := map[string]string{"backend": "opencode", "effort": "high", "guardrail_model": "m1", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCommSetTaskLLM_LegacyUnchanged(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set-task-llm p1 t1 opencode")
	want := map[string]string{"task_id": "t1", "backend": "opencode", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_task_llm" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommSetTaskLLM_GuardrailOnly(t *testing.T) {
	_, got := commSetLLMBody(t, "autonomous set-task-llm p1 t1 guardrail_backend=ollama")
	want := map[string]string{"task_id": "t1", "guardrail_backend": "ollama", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCommSetLLM_UsageWhenNothingToSet(t *testing.T) {
	r := &Router{}
	for _, msg := range []string{"autonomous set-llm p1", "autonomous set-task-llm p1 t1"} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage") || !strings.Contains(got, "guardrail_backend=") {
			t.Errorf("%q: expected usage with guardrail tokens, got %q", msg, got)
		}
	}
}
