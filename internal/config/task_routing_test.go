package config

import (
	"errors"
	"strings"
	"testing"
)

func fakeChecks() TaskRoutingChecks {
	return TaskRoutingChecks{
		BackendKnown: func(b string) bool { return b == "pi-prompt" || b == "claude-code" },
		ModelPairs: func(b, m string) error {
			if b == "claude-code" && strings.Contains(m, "/") {
				return errors.New("mismatch")
			}
			return nil
		},
	}
}

func TestTaskRoutingDefaultTable(t *testing.T) {
	d := DefaultTaskRouting()
	p := d.Profiles[TaskRoutingDefaultProfile]
	for _, ty := range []string{TaskTypeDocsSet, TaskTypeChangelogEntry, TaskTypeParityIssueDraft, TaskTypeLocaleStrings, TaskTypeFormattingCleanup, TaskTypeTests, TaskTypeVersionNote, TaskTypeBoundedLoadStep} {
		if p[ty].WorkerBackend != "pi-prompt" || p[ty].WorkerModel == "" {
			t.Errorf("%s not on pi-prompt: %+v", ty, p[ty])
		}
	}
	for _, ty := range []string{TaskTypeUICode, TaskTypeDaemonBackendCode, TaskTypeMCPAPISurface, TaskTypeMergeConflictRepair, TaskTypeReviewRepair} {
		if p[ty].WorkerBackend != "claude-code" || p[ty].WorkerModel != "sonnet" {
			t.Errorf("%s not on claude-code sonnet: %+v", ty, p[ty])
		}
	}
	if p[TaskTypeFinalGate] != (TaskRoutingRow{}) {
		t.Errorf("final_gate must follow the global policy: %+v", p[TaskTypeFinalGate])
	}
	for ty, r := range p {
		if isPremiumModelName(r.WorkerModel) || r.VerificationBackend != "" || r.VerificationModel != "" || r.SecondChoice != nil {
			t.Errorf("%s: unexpected content %+v", ty, r)
		}
	}
	if len(p) != len(TaskRoutingTaskTypes()) {
		t.Errorf("default profile covers %d types, want %d", len(p), len(TaskRoutingTaskTypes()))
	}
	if errs := ValidateTaskRouting(d, fakeChecks()); len(errs) != 0 {
		t.Errorf("default table invalid: %v", errs)
	}
}

func TestTaskRoutingValidateProblems(t *testing.T) {
	cfg := DefaultTaskRouting()
	p := cfg.Profiles["default"]
	p[TaskTypeDocsSet] = TaskRoutingRow{WorkerBackend: "nope", WorkerModel: "x"}
	p["bogus_type"] = TaskRoutingRow{WorkerBackend: "pi-prompt"}
	p[TaskTypeTests] = TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "ollama/qwen3.8:27b"}
	cfg.ActiveProfile = "missing"
	errs := ValidateTaskRouting(cfg, fakeChecks())
	if len(errs) < 4 {
		t.Fatalf("want every problem reported, got %v", errs)
	}
}

func TestTaskRoutingPremiumModelNeedsExplicitRow(t *testing.T) {
	for _, m := range []string{"claude-opus-5-5", "opus", "claude-fable-5-2"} {
		cfg := DefaultTaskRouting()
		cfg.Profiles["default"][TaskTypeUICode] = TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: m}
		if len(ValidateTaskRouting(cfg, fakeChecks())) == 0 {
			t.Errorf("%s accepted without explicit flag", m)
		}
		cfg.Profiles["default"][TaskTypeUICode] = TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: m, AllowPremiumModel: true}
		if errs := ValidateTaskRouting(cfg, fakeChecks()); len(errs) != 0 {
			t.Errorf("%s refused despite explicit flag: %v", m, errs)
		}
	}
	// A second choice naming opus on a row without the flag is refused.
	cfg := DefaultTaskRouting()
	r := cfg.Profiles["default"][TaskTypeUICode]
	r.SecondChoice = &TaskRoutingChoice{Backend: "claude-code", Model: "opus"}
	cfg.Profiles["default"][TaskTypeUICode] = r
	if len(ValidateTaskRouting(cfg, fakeChecks())) == 0 {
		t.Error("second choice opus accepted implicitly")
	}
}

func TestTaskRoutingWithDefaults(t *testing.T) {
	got := TaskRoutingWithDefaults(TaskRoutingConfig{Enabled: true})
	if !got.Enabled || got.ActiveProfile != "default" || len(got.Profiles) != 1 {
		t.Errorf("defaults not applied: %+v", got)
	}
}
