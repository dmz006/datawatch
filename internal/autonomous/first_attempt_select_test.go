package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Fake tasks, one per attempt kind. taskAttemptKind classifies each one.
func firstAttemptFakeTask(kind string) *Task {
	switch kind {
	case attemptKindFinalGate:
		return &Task{ID: "t-gate", Kind: TaskKindFinalGate, FilesPlanned: []string{"internal/x.go"}}
	case attemptKindNote:
		return &Task{ID: "t-note", ReadOnly: true, FilesPlanned: []string{"internal/x.go"}}
	case attemptKindDocs:
		return &Task{ID: "t-docs", FilesPlanned: []string{"docs/guide.md"}}
	default:
		return &Task{ID: "t-impl", FilesPlanned: []string{"internal/x.go"}}
	}
}

// firstAttemptFakeConfig configures a distinct pair for every kind.
func firstAttemptFakeConfig() Config {
	return Config{FirstAttempt: map[string]config.FirstAttemptChoice{
		attemptKindImplementation: {Backend: "claude-code", Model: "claude-sonnet-5-5"},
		attemptKindDocs:           {Backend: "pi-prompt", Model: "qwen3.8:27b"},
		attemptKindNote:           {Backend: "ollama", Model: "llama-small"},
		attemptKindFinalGate:      {Backend: "claude-code", Model: "claude-opus-5-5"},
	}}
}

func TestFirstAttemptChoice_EachKindPicksItsConfiguredPair(t *testing.T) {
	cfg := firstAttemptFakeConfig()
	cases := []struct {
		kind, backend, model string
	}{
		{attemptKindImplementation, "claude-code", "claude-sonnet-5-5"},
		{attemptKindDocs, "pi-prompt", "qwen3.8:27b"},
		{attemptKindNote, "ollama", "llama-small"},
		{attemptKindFinalGate, "claude-code", "claude-opus-5-5"},
	}
	for _, c := range cases {
		task := firstAttemptFakeTask(c.kind)
		if got := taskAttemptKind(task); got != c.kind {
			t.Fatalf("fake task for %s classified as %s", c.kind, got)
		}
		backend, model, reason, ok := firstAttemptChoice(cfg, task, 0)
		if !ok || backend != c.backend || model != c.model {
			t.Fatalf("%s: got backend=%q model=%q ok=%v, want %q/%q ok=true", c.kind, backend, model, ok, c.backend, c.model)
		}
		if want := "first attempt for " + c.kind + " tasks"; reason != want {
			t.Fatalf("%s: reason=%q want %q", c.kind, reason, want)
		}
	}
}

func TestFirstAttemptChoice_UnsetKindKeepsTodaysChoice(t *testing.T) {
	cfg := Config{FirstAttempt: map[string]config.FirstAttemptChoice{
		attemptKindDocs: {Backend: "pi-prompt", Model: "qwen3.8:27b"},
	}}
	task := firstAttemptFakeTask(attemptKindImplementation)
	if _, _, _, ok := firstAttemptChoice(cfg, task, 0); ok {
		t.Fatal("implementation has no configured pair; firstAttemptChoice must return ok=false")
	}
	backend, model, source, reason := applyFirstAttempt(cfg, task, 0, "opencode", "m-default", ExecutorSourceGlobal)
	if backend != "opencode" || model != "m-default" || source != ExecutorSourceGlobal || reason != "" {
		t.Fatalf("unset kind changed the worker: %q %q %q reason=%q", backend, model, source, reason)
	}
}

func TestFirstAttemptChoice_EmptyBackendEntryKeepsTodaysChoice(t *testing.T) {
	cfg := Config{FirstAttempt: map[string]config.FirstAttemptChoice{
		attemptKindNote: {Backend: "  ", Model: "ignored"},
	}}
	if _, _, _, ok := firstAttemptChoice(cfg, firstAttemptFakeTask(attemptKindNote), 0); ok {
		t.Fatal("a blank backend must not count as a configured pair")
	}
}

func TestFirstAttemptChoice_EmptyModelIsBackendDefault(t *testing.T) {
	cfg := Config{FirstAttempt: map[string]config.FirstAttemptChoice{
		attemptKindImplementation: {Backend: "claude-code"},
	}}
	backend, model, _, ok := firstAttemptChoice(cfg, firstAttemptFakeTask(attemptKindImplementation), 0)
	if !ok || backend != "claude-code" || model != "" {
		t.Fatalf("got backend=%q model=%q ok=%v; want claude-code with an empty model", backend, model, ok)
	}
}

func TestApplyFirstAttempt_PinnedTaskLLMWins(t *testing.T) {
	cfg := firstAttemptFakeConfig()
	task := firstAttemptFakeTask(attemptKindImplementation)
	task.Backend = "opencode"
	task.Model = "operator-pick"
	backend, model, source, reason := applyFirstAttempt(cfg, task, 0, "opencode", "operator-pick", ExecutorSourceTask)
	if backend != "opencode" || model != "operator-pick" || source != ExecutorSourceTask || reason != "" {
		t.Fatalf("pinned task llm was overridden: %q %q %q reason=%q", backend, model, source, reason)
	}
}

func TestApplyFirstAttempt_OtherOperatorPinsAlsoWin(t *testing.T) {
	cfg := firstAttemptFakeConfig()
	task := firstAttemptFakeTask(attemptKindImplementation)
	for _, source := range []string{ExecutorSourceStory, ExecutorSourcePRD, ExecutorSourceAutomation, ExecutorSourceBurnGuard} {
		backend, model, got, reason := applyFirstAttempt(cfg, task, 0, "kept-backend", "kept-model", source)
		if backend != "kept-backend" || model != "kept-model" || got != source || reason != "" {
			t.Fatalf("source %s was overridden: %q %q %q reason=%q", source, backend, model, got, reason)
		}
	}
}

func TestApplyFirstAttempt_FirstAttemptReplacesGlobalDefault(t *testing.T) {
	cfg := firstAttemptFakeConfig()
	task := firstAttemptFakeTask(attemptKindDocs)
	backend, model, source, reason := applyFirstAttempt(cfg, task, 0, "claude-code", "", ExecutorSourceGlobal)
	if backend != "pi-prompt" || model != "qwen3.8:27b" || source != ExecutorSourceFirstAttempt {
		t.Fatalf("first attempt did not use its pair: %q %q %q", backend, model, source)
	}
	if reason == "" {
		t.Fatal("applied first attempt must return a reason for the log")
	}
}

func TestApplyFirstAttempt_SecondAttemptDoesNotUseThePair(t *testing.T) {
	cfg := firstAttemptFakeConfig()
	task := firstAttemptFakeTask(attemptKindImplementation)
	for _, failures := range []int{1, 2, 3} {
		if _, _, _, ok := firstAttemptChoice(cfg, task, failures); ok {
			t.Fatalf("failedAttempts=%d must not use the first-attempt pair", failures)
		}
		backend, model, source, reason := applyFirstAttempt(cfg, task, failures, "claude-code", "m-retry", ExecutorSourceGlobal)
		if backend != "claude-code" || model != "m-retry" || source != ExecutorSourceGlobal || reason != "" {
			t.Fatalf("retry (failures=%d) changed the worker: %q %q %q", failures, backend, model, source)
		}
	}
}
