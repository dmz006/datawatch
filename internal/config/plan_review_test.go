// Plan-review gate (2026-10-17) — config surface: the mode is default-off,
// valid values load, unknown values are rejected at load time, and the
// keys round-trip a save. The smoke-verification backend/model keys are
// config-surface only in this story (a parallel deliverable consumes them).
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanReviewMode_DefaultOff(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  enabled: false\n")
	if cfg.Autonomous.PlanReviewMode != "" {
		t.Fatalf("plan_review_mode = %q, want empty (off by default)", cfg.Autonomous.PlanReviewMode)
	}
	if cfg.Autonomous.PlanReviewBackend != "" || cfg.Autonomous.PlanReviewModel != "" {
		t.Fatalf("backend/model default to zero: %q / %q",
			cfg.Autonomous.PlanReviewBackend, cfg.Autonomous.PlanReviewModel)
	}
	if cfg.Autonomous.SmokeVerificationBackend != "" || cfg.Autonomous.SmokeVerificationModel != "" {
		t.Fatalf("smoke_verification keys default to zero: %q / %q",
			cfg.Autonomous.SmokeVerificationBackend, cfg.Autonomous.SmokeVerificationModel)
	}
}

func TestPlanReviewMode_ValidModes(t *testing.T) {
	for _, mode := range []string{"off", "warn", "block"} {
		cfg := loadYAMLBody(t, "autonomous:\n  plan_review_mode: "+mode+"\n  plan_review_backend: ollama\n  plan_review_model: judge\n  smoke_verification_backend: ollama\n  smoke_verification_model: runner\n")
		if cfg.Autonomous.PlanReviewMode != mode {
			t.Fatalf("mode = %q, want %q", cfg.Autonomous.PlanReviewMode, mode)
		}
		if cfg.Autonomous.PlanReviewBackend != "ollama" || cfg.Autonomous.PlanReviewModel != "judge" {
			t.Fatalf("reviewer backend/model = %q/%q, want ollama/judge",
				cfg.Autonomous.PlanReviewBackend, cfg.Autonomous.PlanReviewModel)
		}
		if cfg.Autonomous.SmokeVerificationBackend != "ollama" || cfg.Autonomous.SmokeVerificationModel != "runner" {
			t.Fatalf("smoke keys = %q/%q, want ollama/runner",
				cfg.Autonomous.SmokeVerificationBackend, cfg.Autonomous.SmokeVerificationModel)
		}
	}
}

func TestPlanReviewMode_InvalidValueRejected(t *testing.T) {
	for _, bad := range []string{"yes", "BLOCK", "block ", " warn", "block warn", "x"} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		body := "autonomous:\n  plan_review_mode: '" + bad + "'\n"
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("Load accepted invalid plan_review_mode %q, want error", bad)
		}
	}
}

func TestPlanReviewMode_SaveRoundTrip(t *testing.T) {
	cfg := loadYAMLBody(t, "autonomous:\n  plan_review_mode: block\n  plan_review_backend: openai\n  plan_review_model: gpt-judge\n  smoke_verification_backend: ollama\n  smoke_verification_model: runner\n")
	out := filepath.Join(t.TempDir(), "out.yaml")
	if err := Save(cfg, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plan_review_mode: block", "smoke_verification_backend: ollama"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("saved YAML lacks %q:\n%s", want, raw)
		}
	}
	again, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.Autonomous.PlanReviewMode != "block" || again.Autonomous.PlanReviewBackend != "openai" || again.Autonomous.PlanReviewModel != "gpt-judge" ||
		again.Autonomous.SmokeVerificationBackend != "ollama" || again.Autonomous.SmokeVerificationModel != "runner" {
		t.Fatalf("round trip lost values: %+v", again.Autonomous)
	}
}
