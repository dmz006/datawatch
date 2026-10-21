// Table-driven tests for guardrail judge model resolution
// (guardrail_model.go, v9.10.18).
//
// The invariant under test: a guardrail judge (rules-review, ci-watch,
// rejudge, per-task / per-story guardrails) always gets a model that was
// configured at an explicit level — PRD guardrail_model → task
// guardrail_model → autonomous.guardrail_model → autonomous.verification_model
// → autonomous.planning_model — paired with the resolved guardrail backend.
// When nothing configured can be served, resolution fails with
// ErrNoGuardrailModel and the caller records a loud, non-retried
// infra-category block (NoGuardrailModelVerdict) instead of launching a
// judge on the backend's built-in default model (the exact bug: an
// unconfigured pi judge silently fell back to the registry default,
// evicting the operator's pinned model on the node).
package autonomous

import (
	"errors"
	"strings"
	"testing"
)

// TestResolveGuardrailModel_Coverage pins every precedence tier, the
// empty-everywhere failure, the pairing skips, and the claude-code
// non-regression for ResolveGuardrailModel.
//
// Precedence tiers (first match wins), for a pi-family backend:
//
//  1. PRD guardrail_model (prd argument)
//  2. task guardrail_model (task argument)
//  3. autonomous.guardrail_model (globalGuardrailModel)
//  4. autonomous.verification_model (globalVerificationModel)
//  5. autonomous.planning_model (globalPlanningModel)
func TestResolveGuardrailModel_Coverage(t *testing.T) {
	const (
		piPin         = "ollama/qwen3.8-64k:27b"
		piPinAlt      = "ollama/qwen3.8:27b"
		otherProvider = "openai/gpt-5"
		bareClaude    = "claude-opus-5-5"
	)
	cases := []struct {
		name       string
		backend    string
		prd, task  string
		gGuardrail string
		gVerified  string
		gPlanning  string
		wantModel  string
		wantErr    bool
		errSubstr  []string // each must appear in the error text
	}{
		{
			name:       "tier1 PRD pin wins over task and all globals",
			backend:    "pi",
			prd:        piPin,
			task:       piPinAlt,
			gGuardrail: otherProvider, gVerified: "ollama/qwen3-mini", gPlanning: "ollama/qwen-tiny",
			wantModel: piPin,
		},
		{
			name:       "tier2 task override wins when PRD empty",
			backend:    "pi",
			task:       piPin,
			gGuardrail: otherProvider, gVerified: "ollama/qwen3-mini", gPlanning: "ollama/qwen-tiny",
			wantModel: piPin,
		},
		{
			name:       "tier3 global guardrail_model wins over verification and planning",
			backend:    "pi",
			gGuardrail: piPin,
			gVerified:  piPinAlt,
			gPlanning:  "ollama/qwen-tiny",
			wantModel:  piPin,
		},
		{
			name:       "task tier (2) wins when PRD empty, over global tier3",
			backend:    "pi",
			task:       piPinAlt,
			gGuardrail: otherProvider,
			wantModel:  piPinAlt,
		},
		{
			name:      "tier4 global verification_model when no guardrail_model",
			backend:   "pi",
			gVerified: piPin,
			gPlanning: "ollama/qwen-tiny",
			wantModel: piPin,
		},
		{
			name:      "tier5 global planning_model is the last global tier",
			backend:   "pi",
			gPlanning: piPin,
			wantModel: piPin,
		},
		{
			name:      "pi backend accepts any provider-qualified pin at every tier",
			backend:   "pi",
			prd:       otherProvider,
			gVerified: piPin,
			wantModel: otherProvider,
		},
		{
			name:      "pi-prompt backend serves provider pins too",
			backend:   "pi-prompt",
			gVerified: piPin,
			wantModel: piPin,
		},
		{
			name:       "bare Claude alias is skipped on pi but next valid tier resolves",
			backend:    "pi",
			prd:        bareClaude,
			gGuardrail: piPin,
			wantModel:  piPin,
			errSubstr:  nil, // resolves, no error
		},
		{
			name:      "pairing skip of a bare alias is reported in the error when nothing else resolves",
			backend:   "pi",
			prd:       bareClaude,
			task:      "claude-haiku-5-5",
			gVerified: "claude-opus-5-5",
			wantErr:   true,
			errSubstr: []string{"claude-haiku-5-5", "task guardrail_model"},
		},
		{
			name:      "empty backend pairs as any — PRD tier returns its model",
			backend:   "",
			prd:       piPin,
			gVerified: bareClaude,
			wantModel: piPin,
		},
		{
			name:       "whitespace-trimmed tiers are accepted and returned trimmed",
			backend:    "pi",
			gGuardrail: "  " + piPin + "  ",
			wantModel:  piPin,
		},
		{
			name:    "empty-everywhere on pi is an infra failure, never a backend default",
			backend: "pi",
			wantErr: true,
			errSubstr: []string{
				"PRD guardrail_model", "task guardrail_model",
				"autonomous.guardrail_model",
				"autonomous.verification_model",
				"autonomous.planning_model",
				"resolved backend \"pi\"",
			},
		},
		{
			name:       "claude-code backend: bare Claude alias (tier1) resolves despite pi global pins",
			backend:    claudeCodeBackendName,
			prd:        bareClaude,
			gGuardrail: piPin, gVerified: piPinAlt, gPlanning: "ollama/qwen-tiny",
			wantModel: bareClaude,
		},
		{
			name:       "claude-code backend: task bare Claude alias (tier2) resolves despite pi global pins",
			backend:    claudeCodeBackendName,
			task:       bareClaude,
			gGuardrail: piPin, gVerified: piPinAlt,
			wantModel: bareClaude,
		},
		{
			name:       "claude-code backend never receives a provider-prefixed model (non-regression)",
			backend:    claudeCodeBackendName,
			prd:        piPin,
			task:       piPinAlt,
			gGuardrail: piPin, gVerified: piPinAlt, gPlanning: "ollama/qwen-tiny",
			wantErr:   true, // every tier is pi-specific for claude-code
			errSubstr: []string{"pi-specific global, skipped on the claude-code backend"},
		},
		{
			name:      "claude-code backend empty-everywhere fails loudly, never a backend default",
			backend:   claudeCodeBackendName,
			wantErr:   true,
			errSubstr: []string{"configured"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, err := ResolveGuardrailModel(
				tc.backend, tc.prd, tc.task,
				tc.gGuardrail, tc.gVerified, tc.gPlanning,
			)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want ErrNoGuardrailModel, got model %q, nil error", model)
				}
				if !errors.Is(err, ErrNoGuardrailModel) {
					t.Fatalf("error = %v; want errors.Is(err, ErrNoGuardrailModel)", err)
				}
				if model != "" {
					t.Fatalf("on failure model = %q; want empty string", model)
				}
				for _, sub := range tc.errSubstr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error text missing %q; got: %v", sub, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if model != tc.wantModel {
				t.Fatalf("model = %q; want %q", model, tc.wantModel)
			}
		})
	}
}

// TestResolveGuardrailModel_NeverEmptyForPiBackend is the headline
// regression: before this file's resolver, "empty everywhere" produced an
// empty model and the pi execution path fell back to the backend's
// built-in (registry) default model — silently evicting the pinned model
// on the node. Now that shape is a loud, typed failure.
func TestResolveGuardrailModel_NeverEmptyForPiBackend(t *testing.T) {
	for _, backend := range []string{"pi", "pi-prompt"} {
		t.Run(backend, func(t *testing.T) {
			model, err := ResolveGuardrailModel(backend, "", "", "", "", "")
			if model != "" {
				t.Fatalf("model = %q; a pi-family judge must never resolve to an empty model (registry default)", model)
			}
			if !errors.Is(err, ErrNoGuardrailModel) {
				t.Fatalf("err = %v; want ErrNoGuardrailModel", err)
			}
		})
	}
}

// TestResolveUnitGuardrailModelLocked covers the pure core that pairs the
// unit-level backend precedence (task → PRD → global
// autonomous.verification_backend) with the model tiers over a config
// snapshot.
func TestResolveUnitGuardrailModelLocked(t *testing.T) {
	cases := []struct {
		name        string
		cfg         Config
		task        *Task
		prd         *PRD
		wantBackend string
		wantModel   string
		wantErr     bool
	}{
		{
			name:        "task backend + task model pin (most specific level)",
			cfg:         Config{VerificationBackend: "claude-code", VerificationModel: "ollama/qwen3.8:27b"},
			task:        &Task{GuardrailBackend: "pi", GuardrailModel: "ollama/qwen3.8-64k:27b"},
			wantBackend: "pi",
			wantModel:   "ollama/qwen3.8-64k:27b",
		},
		{
			name:        "PRD backend + global guardrail_model tier over a PRD model it can pair with",
			cfg:         Config{VerificationBackend: "ollama", GuardrailModel: "anthropic/claude-haiku-5-5"},
			prd:         &PRD{GuardrailBackend: "pi", GuardrailModel: "ollama/qwen3.8:27b"},
			wantBackend: "pi",
			wantModel:   "ollama/qwen3.8:27b",
		},
		{
			name:        "global verification_backend default with global tier3 model",
			cfg:         Config{VerificationBackend: "pi", GuardrailModel: "ollama/qwen3.8-64k:27b"},
			wantBackend: "pi",
			wantModel:   "ollama/qwen3.8-64k:27b",
		},
		{
			name:        "verification_model is tier4 when guardrail_model empty",
			cfg:         Config{VerificationBackend: "pi", VerificationModel: "ollama/qwen3.8:27b"},
			wantBackend: "pi",
			wantModel:   "ollama/qwen3.8:27b",
		},
		{
			name:        "planning_model is the last global tier",
			cfg:         Config{VerificationBackend: "pi", PlanningModel: "ollama/qwen-tiny"},
			wantBackend: "pi",
			wantModel:   "ollama/qwen-tiny",
		},
		{
			name:        "nothing configured anywhere: typed failure, backend still resolved",
			cfg:         Config{VerificationBackend: "pi"},
			wantErr:     true,
			wantBackend: "pi",
		},
		{
			name: "claude-code task backend with a pi model pin fails instead of leaking it",
			cfg:  Config{VerificationBackend: "pi", VerificationModel: "ollama/qwen3.8:27b"},
			task: &Task{GuardrailBackend: claudeCodeBackendName, GuardrailModel: "ollama/qwen3.8:27b"},
			// tier1 cannot pair (pi-qualified name on claude-code); tiers
			// 3–5 are skipped on claude-code; loud failure.
			wantErr:     true,
			wantBackend: claudeCodeBackendName,
		},
		{
			name:        "nil task and nil PRD are safe",
			cfg:         Config{VerificationBackend: "pi", GuardrailModel: "ollama/qwen3.8-64k:27b"},
			task:        nil,
			prd:         nil,
			wantBackend: "pi",
			wantModel:   "ollama/qwen3.8-64k:27b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, model, err := resolveUnitGuardrailModelLocked(tc.cfg, tc.task, tc.prd)
			if backend != tc.wantBackend {
				t.Fatalf("backend = %q; want %q", backend, tc.wantBackend)
			}
			if tc.wantErr {
				if !errors.Is(err, ErrNoGuardrailModel) {
					t.Fatalf("err = %v; want ErrNoGuardrailModel", err)
				}
				if model != "" {
					t.Fatalf("model = %q on failure; want empty", model)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if model != tc.wantModel {
				t.Fatalf("model = %q; want %q", model, tc.wantModel)
			}
		})
	}
}

// TestManagerResolveUnitGuardrailModel covers the lock-owning Manager
// method end to end on a real manager.
func TestManagerResolveUnitGuardrailModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VerificationBackend = "pi"
	cfg.GuardrailModel = "ollama/qwen3.8-64k:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	backend, model, err := m.resolveUnitGuardrailModel(
		&Task{GuardrailModel: "ollama/qwen3.8:27b"}, (&PRD{}),
	)
	if err != nil {
		t.Fatalf("resolveUnitGuardrailModel: %v", err)
	}
	if backend != "pi" || model != "ollama/qwen3.8:27b" {
		t.Fatalf("resolved %q/%q; want pi/ollama/qwen3.8:27b (task tier beats global tier3)", backend, model)
	}

	cfg2 := DefaultConfig()
	cfg2.VerificationBackend = "pi" // nothing else configured
	m2, err := NewManager(t.TempDir(), cfg2, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, _, err := m2.resolveUnitGuardrailModel(nil, nil); !errors.Is(err, ErrNoGuardrailModel) {
		t.Fatalf("err = %v; want ErrNoGuardrailModel", err)
	}
}

// TestNoGuardrailModelVerdict pins the loud, non-retried, infra-category
// block shape the callers record when model resolution fails.
func TestNoGuardrailModelVerdict(t *testing.T) {
	cause := errors.New("no guardrail judge model configured (boom)")
	v := NoGuardrailModelVerdict("rules-review", cause)
	if v.Outcome != "block" {
		t.Fatalf("Outcome = %q; want block", v.Outcome)
	}
	if v.Category != "infra" {
		t.Fatalf("Category = %q; want infra (the only category the infra-retry discipline would touch)", v.Category)
	}
	if v.Guardrail != "rules-review" {
		t.Fatalf("Guardrail = %q; want rules-review", v.Guardrail)
	}
	if v.Severity == "" {
		t.Fatalf("Severity must be set for a blocking verdict")
	}
	sum := v.Summary
	for _, sub := range []string{"could not run", "no judge model", "configuration failure"} {
		if !strings.Contains(sum, sub) {
			t.Errorf("Summary %q missing %q", sum, sub)
		}
	}
	if len(v.Issues) == 0 {
		t.Fatalf("Issues must name the operator remedy")
	}
	if !strings.Contains(v.Issues[0], "autonomous.guardrail_model") {
		t.Fatalf("Issues[0] %q must name the config keys to set; got %q", v.Issues[0], v.Issues)
	}
	if v.VerdictAt.IsZero() {
		t.Fatalf("VerdictAt must be set")
	}
}
