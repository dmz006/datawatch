// comm `autonomous set-task-verification-llm` / `set-story-verification-llm`
// / `set-verification-llm` key=value parsing.

package router

import (
	"reflect"
	"strings"
	"testing"
)

func TestCommSetTaskVerificationLLM(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set-task-verification-llm p1 t1 backend=ollama model=")
	want := map[string]string{"task_id": "t1", "verification_backend": "ollama", "verification_model": "", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_task_verification_llm" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommSetStoryVerificationLLM_Underscore(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set_story_verification_llm p1 s1 effort=high")
	want := map[string]string{"story_id": "s1", "verification_effort": "high", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_story_verification_llm" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommSetVerificationLLM(t *testing.T) {
	path, got := commSetLLMBody(t, "autonomous set-verification-llm p1 model=qwen3 effort=low")
	want := map[string]string{"verification_model": "qwen3", "verification_effort": "low", "actor": "operator"}
	if path != "/api/autonomous/prds/p1/set_verification_llm" || !reflect.DeepEqual(got, want) {
		t.Errorf("path=%s body=%v want %v", path, got, want)
	}
}

func TestCommVerificationLLM_Usage(t *testing.T) {
	r := &Router{}
	for _, msg := range []string{
		"autonomous set-verification-llm p1",
		"autonomous set-task-verification-llm p1 t1",
		"autonomous set-story-verification-llm p1",
		"autonomous set-verification-llm p1 bogus=1",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage") || !strings.Contains(got, "backend=") {
			t.Errorf("%q: expected usage, got %q", msg, got)
		}
	}
}
