package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttemptCost_Defaults(t *testing.T) {
	def := DefaultConfig().Autonomous
	if def.FirstAttemptEscalateAfterFailures != 1 {
		t.Fatalf("DefaultConfig first_attempt_escalate_after_failures = %d, want 1", def.FirstAttemptEscalateAfterFailures)
	}
	if def.RetryResumeMaxContextTokens != 150000 {
		t.Fatalf("DefaultConfig retry_resume_max_context_tokens = %d, want 150000", def.RetryResumeMaxContextTokens)
	}
	if len(def.FirstAttempt) != 0 {
		t.Fatalf("DefaultConfig first_attempt = %v, want empty (today's choice)", def.FirstAttempt)
	}
	if err := ValidateAttemptCostSettings(def); err != nil {
		t.Fatalf("defaults must validate, got %v", err)
	}

	// Truly empty file: every value comes from DefaultConfig.
	empty := loadYAML(t, "")
	if empty.Autonomous.FirstAttemptEscalateAfterFailures != 1 {
		t.Fatalf("empty config escalate_after = %d, want 1", empty.Autonomous.FirstAttemptEscalateAfterFailures)
	}
	if empty.Autonomous.RetryResumeMaxContextTokens != 150000 {
		t.Fatalf("empty config resume context = %d, want 150000", empty.Autonomous.RetryResumeMaxContextTokens)
	}
	if len(empty.Autonomous.FirstAttempt) != 0 {
		t.Fatalf("empty config first_attempt = %v, want empty", empty.Autonomous.FirstAttempt)
	}

	// Absent keys under a populated autonomous section keep their defaults.
	populated := loadYAML(t, "autonomous:\n  enabled: true\n")
	if populated.Autonomous.FirstAttemptEscalateAfterFailures != 1 {
		t.Fatalf("absent escalate_after = %d, want default 1", populated.Autonomous.FirstAttemptEscalateAfterFailures)
	}
	if populated.Autonomous.RetryResumeMaxContextTokens != 150000 {
		t.Fatalf("absent resume context = %d, want default 150000", populated.Autonomous.RetryResumeMaxContextTokens)
	}
}

func TestAttemptCost_ValidMapLoads(t *testing.T) {
	body := `autonomous:
  first_attempt:
    implementation:
      backend: claude-code
    docs:
      backend: pi-prompt
      model: qwen3.8:27b
    note: {}
    final_gate:
      backend: ollama
      model: qwen3.8:27b
  first_attempt_escalate_after_failures: 2
  retry_resume_max_context_tokens: 80000
`
	cfg := loadYAML(t, body)
	a := cfg.Autonomous
	if got := a.FirstAttempt["implementation"]; got.Backend != "claude-code" || got.Model != "" {
		t.Fatalf("implementation = %+v, want backend claude-code with no model", got)
	}
	if got := a.FirstAttempt["docs"]; got.Backend != "pi-prompt" || got.Model != "qwen3.8:27b" {
		t.Fatalf("docs = %+v, want pi-prompt / qwen3.8:27b", got)
	}
	if got, ok := a.FirstAttempt["note"]; !ok || got != (FirstAttemptChoice{}) {
		t.Fatalf("note = %+v (present %v), want empty entry kept", got, ok)
	}
	if got := a.FirstAttempt["final_gate"]; got.Backend != "ollama" || got.Model != "qwen3.8:27b" {
		t.Fatalf("final_gate = %+v, want ollama / qwen3.8:27b", got)
	}
	if a.FirstAttemptEscalateAfterFailures != 2 {
		t.Fatalf("escalate_after = %d, want 2", a.FirstAttemptEscalateAfterFailures)
	}
	if a.RetryResumeMaxContextTokens != 80000 {
		t.Fatalf("resume context = %d, want 80000", a.RetryResumeMaxContextTokens)
	}
	if err := ValidateAttemptCostSettings(a); err != nil {
		t.Fatalf("valid map must pass, got %v", err)
	}
}

func TestAttemptCost_ZeroIsMeaningfulAndRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  retry_resume_max_context_tokens: 0\n  first_attempt_escalate_after_failures: 0\n")
	if cfg.Autonomous.RetryResumeMaxContextTokens != 0 {
		t.Fatalf("resume context = %d, want explicit 0 (always fresh) preserved", cfg.Autonomous.RetryResumeMaxContextTokens)
	}
	if cfg.Autonomous.FirstAttemptEscalateAfterFailures != 0 {
		t.Fatalf("escalate_after = %d, want explicit 0 preserved", cfg.Autonomous.FirstAttemptEscalateAfterFailures)
	}
	p := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if again.Autonomous.RetryResumeMaxContextTokens != 0 {
		t.Fatalf("after round trip resume context = %d, want 0", again.Autonomous.RetryResumeMaxContextTokens)
	}
	if again.Autonomous.FirstAttemptEscalateAfterFailures != 0 {
		t.Fatalf("after round trip escalate_after = %d, want 0", again.Autonomous.FirstAttemptEscalateAfterFailures)
	}
}

func TestAttemptCost_BoundariesAccepted(t *testing.T) {
	for _, a := range []AutonomousConfig{
		{FirstAttemptEscalateAfterFailures: 0, RetryResumeMaxContextTokens: 0},
		{FirstAttemptEscalateAfterFailures: 20, RetryResumeMaxContextTokens: 2000000},
	} {
		if err := ValidateAttemptCostSettings(a); err != nil {
			t.Fatalf("boundary %+v must pass, got %v", a, err)
		}
	}
}

func TestAttemptCost_RefusesBadSettings(t *testing.T) {
	base := AutonomousConfig{FirstAttemptEscalateAfterFailures: 1, RetryResumeMaxContextTokens: 150000}
	with := func(mutate func(*AutonomousConfig)) AutonomousConfig {
		a := base
		mutate(&a)
		return a
	}
	cases := []struct {
		name    string
		cfg     AutonomousConfig
		wantKey string
	}{
		{
			name: "unknown task kind",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"review": {Backend: "claude-code"}}
			}),
			wantKey: "autonomous.first_attempt",
		},
		{
			name: "task kind is case-sensitive",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"Implementation": {Backend: "claude-code"}}
			}),
			wantKey: "autonomous.first_attempt",
		},
		{
			name: "empty task kind",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"": {}}
			}),
			wantKey: "autonomous.first_attempt",
		},
		{
			name: "pi-prompt without a model",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"docs": {Backend: "pi-prompt"}}
			}),
			wantKey: "autonomous.first_attempt.docs.model",
		},
		{
			name: "pi without a model, whitespace only",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"implementation": {Backend: "pi", Model: "   "}}
			}),
			wantKey: "autonomous.first_attempt.implementation.model",
		},
		{
			name: "ollama without a model",
			cfg: with(func(a *AutonomousConfig) {
				a.FirstAttempt = map[string]FirstAttemptChoice{"final_gate": {Backend: "Ollama"}}
			}),
			wantKey: "autonomous.first_attempt.final_gate.model",
		},
		{
			name:    "negative escalate-after",
			cfg:     with(func(a *AutonomousConfig) { a.FirstAttemptEscalateAfterFailures = -1 }),
			wantKey: "autonomous.first_attempt_escalate_after_failures",
		},
		{
			name:    "escalate-after above 20",
			cfg:     with(func(a *AutonomousConfig) { a.FirstAttemptEscalateAfterFailures = 21 }),
			wantKey: "autonomous.first_attempt_escalate_after_failures",
		},
		{
			name:    "negative resume context",
			cfg:     with(func(a *AutonomousConfig) { a.RetryResumeMaxContextTokens = -1 }),
			wantKey: "autonomous.retry_resume_max_context_tokens",
		},
		{
			name:    "resume context above 2000000",
			cfg:     with(func(a *AutonomousConfig) { a.RetryResumeMaxContextTokens = 2000001 }),
			wantKey: "autonomous.retry_resume_max_context_tokens",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAttemptCostSettings(tc.cfg)
			if err == nil {
				t.Fatalf("expected refusal, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Fatalf("error %q does not name key %q", err.Error(), tc.wantKey)
			}
		})
	}
}

func TestAttemptCost_LoadRefusesBadValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  retry_resume_max_context_tokens: -5\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatalf("Load accepted a negative retry_resume_max_context_tokens")
	}
	if !strings.Contains(err.Error(), "autonomous.retry_resume_max_context_tokens") {
		t.Fatalf("Load error %q does not name the key", err.Error())
	}
}
