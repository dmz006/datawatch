// Tests for the v9.10.19 pi-family model pinning guards (see
// pi_family_model_guard.go): a pi / pi-prompt session must run with an
// explicit, non-empty model or the caller receives a loud verdict —
// never the harness's built-in default. Non-pi backends keep their
// existing behavior.
package main

import (
	"strings"
	"testing"
)

func TestGuardPiFamilySessionModel(t *testing.T) {
	tests := []struct {
		name          string
		gkind         string
		gbackend      string
		guardrailName string
		unitModel     string
		cfgGuardrail  string
		cfgVerif      string
		cfgPlanning   string
		nonPiFallback string
		wantModel     string
		wantVerdict   bool
	}{
		{
			// a) the invocation's pre-resolved model wins for pi.
			name:      "pi uses the pre-resolved model",
			gkind:     "pi",
			gbackend:  "pi",
			unitModel: "prov/model",
			wantModel: "prov/model",
		},
		{
			// b) empty unit model: global tiers resolve the pin; never
			// the non-pi fallback (registry default) — that is the
			// exact anti-pattern v9.10.19 removes.
			name:        "pi empty unit falls back to global tiers, not the registry default",
			gkind:       "pi",
			gbackend:    "pi",
			unitModel:   "",
			cfgVerif:    "prov/verifier-model:64k",
			wantModel:   "prov/verifier-model:64k",
			wantVerdict: false,
		},
		{
			// c) nothing set anywhere: loud infra block, not a
			// session on the harness default.
			name:          "pi empty unit, empty globals: block verdict naming the keys",
			gkind:         "pi",
			gbackend:      "pi",
			guardrailName: "bl25",
			unitModel:     "",
			wantVerdict:   true,
		},
		{
			// d) same for the one-shot kind.
			name:          "pi-prompt nothing set: block verdict",
			gkind:         "pi-prompt",
			gbackend:      "pi-prompt",
			guardrailName: "ci-watch",
			wantVerdict:   true,
		},
		{
			// e) a bare claude alias paired for claude-code is not a
			// model the pi backend can serve: it is skipped, and the
			// global tiers still resolve a pin.
			name:        "pi skips an unpaired alias, tier after it wins",
			gkind:       "pi",
			gbackend:    "pi",
			unitModel:   "claude-haiku-5",
			cfgPlanning: "prov/planning-model",
			wantModel:   "prov/planning-model",
		},
		{
			// f) unpaired unit model AND unpaired/empty globals:
			// block, naming the failure.
			name:          "pi only an unpaired alias set: block verdict",
			gkind:         "pi",
			gbackend:      "pi",
			guardrailName: "rules-review",
			unitModel:     "claude-haiku-5",
			wantVerdict:   true,
		},
		{
			// g) non-pi session kind: per-unit override passes through
			// unchanged (B124 follow-up behavior untouched).
			name:        "claude-code override passes through",
			gkind:       "claude-code",
			gbackend:    "claude-code",
			unitModel:   "claude-opus-5-5",
			wantModel:   "claude-opus-5-5",
		},
		{
			// h) non-pi session kind without an override: the
			// registry-default fallback still applies for them.
			name:          "claude-code no override keeps the registry default",
			gkind:         "claude-code",
			gbackend:      "claude-code",
			unitModel:     "",
			nonPiFallback: "registry-default-model",
			wantModel:     "registry-default-model",
		},
		{
			// i) non-pi kind is never vetoed by the pinning guard,
			// even when the global tiers are unset.
			name:        "opencode no override: fallback, no verdict",
			gkind:       "opencode",
			gbackend:    "opencode",
			unitModel:   "",
			nonPiFallback: "ollama/whatever",
			wantModel: "ollama/whatever",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, verdict := guardPiFamilySessionModel(
				tt.gkind, tt.gbackend, tt.guardrailName, tt.unitModel,
				tt.cfgGuardrail, tt.cfgVerif, tt.cfgPlanning, tt.nonPiFallback,
			)
			if (verdict != nil) != tt.wantVerdict {
				t.Fatalf("verdict = %v, wantVerdict %v", verdict, tt.wantVerdict)
			}
			if verdict != nil {
				if verdict.Outcome != "block" || verdict.Category != "infra" || verdict.Severity != "high" {
					t.Errorf("verdict shape = %+v, want block/infra/high", *verdict)
				}
				if !strings.Contains(verdict.Summary, "configuration failure") {
					t.Errorf("verdict summary should say it is a configuration failure: %q", verdict.Summary)
				}
				for _, key := range []string{"guardrail_model", "verification_model", "planning_model"} {
					if !strings.Contains(strings.Join(verdict.Issues, " "), key) {
						t.Errorf("verdict issues should name a %q config key: %v", key, verdict.Issues)
					}
				}
				return
			}
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
			if model == "" {
				t.Errorf("pi-family guard must never hand the session an empty model")
			}
		})
	}
}

func TestGuardPiFamilyVerifyModel(t *testing.T) {
	tests := []struct {
		name        string
		vbackend    string
		cfgVerif    string
		cfgPlanning string
		wantModel   string
		wantSetup   bool
	}{
		{
			name:      "verification_model pair wins",
			vbackend:  "pi",
			cfgVerif:  "prov/verifier-model:64k",
			wantModel: "prov/verifier-model:64k",
		},
		{
			name:      "planning_model resolves when verification_model is unset",
			vbackend:  "pi-prompt",
			cfgPlanning: "prov/planning-model",
			wantModel: "prov/planning-model",
		},
		{
			// unpaired alias in the first tier: skipped, the tier
			// after it still resolves — and the error (when nothing
			// pairs) names every configured key.
			name: "unpaired alias skipped, planning_model wins",
			vbackend:  "pi",
			cfgVerif:  "claude-haiku-5",
			cfgPlanning: "prov/planning-model",
			wantModel: "prov/planning-model",
		},
		{
			name:      "nothing set: setup verdict naming the keys",
			vbackend:  "pi",
			wantSetup: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, setup := guardPiFamilyVerifyModel(tt.vbackend, tt.cfgVerif, tt.cfgPlanning)
			if (setup != nil) != tt.wantSetup {
				t.Fatalf("setup = %v, wantSetup %v", setup, tt.wantSetup)
			}
			if setup != nil {
				if setup.OK || setup.Category != "setup" {
					t.Errorf("setup verdict = %+v, want OK=false Category=setup", *setup)
				}
				joined := strings.Join(setup.Issues, " ")
				for _, key := range []string{"verification_model", "planning_model", "PRD model"} {
					if !strings.Contains(joined, key) {
						t.Errorf("setup issues should name %q: %v", key, setup.Issues)
					}
				}
				return
			}
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
		})
	}

	// The setup verdict must be the non-retry class the executor maps
	// to an immediate block (zero auto-fix budget) — assert the
	// executor's own decision key, not just the string.
	if _, setup := guardPiFamilyVerifyModel("pi", "", ""); setup != nil && setup.Category != "setup" {
		t.Errorf("missing-model verdict category = %q, want setup (executor.go blocks on \"setup\")", setup.Category)
	}
}
