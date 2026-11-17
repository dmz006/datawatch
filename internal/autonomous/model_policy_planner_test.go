package autonomous

import (
	"errors"
	"testing"
)

// TestClaudeExplicitModelJudge pins the explicit-model policy
// (model_policy.go) at the guardrail judge: a claude-code judge with no
// model named at any level resolves to the daemon default — sonnet; an
// operator-named premium model passes through unchanged; named pins the
// backend cannot serve still fail loudly (the v9.10.18 pin discipline);
// and pi-family judges are untouched, still failing loudly with no
// backend default on an empty model.
func TestClaudeExplicitModelJudge(t *testing.T) {
	t.Run("claude-code judge with no model anywhere resolves to the daemon default sonnet", func(t *testing.T) {
		model, err := ResolveGuardrailModel(claudeCodeBackendName, "", "", "", "", "")
		if err != nil {
			t.Fatalf("resolve = err %v; an unconfigured claude-code judge must default to the daemon model %q, not fail", err, DefaultClaudeModel)
		}
		if model != DefaultClaudeModel {
			t.Fatalf("model = %q, want the daemon default %q", model, DefaultClaudeModel)
		}
	})

	t.Run("claude-code unit path: global claude-code backend, every tier empty, defaults to sonnet unpinned", func(t *testing.T) {
		cfg := Config{VerificationBackend: claudeCodeBackendName}
		backend, model, pinned, err := resolveUnitGuardrailModelLocked(cfg, nil, nil)
		if err != nil {
			t.Fatalf("unit resolve = err %v; want the daemon default %q", err, DefaultClaudeModel)
		}
		if backend != claudeCodeBackendName || model != DefaultClaudeModel {
			t.Fatalf("got (%q, %q), want (%q, %q)", backend, model, claudeCodeBackendName, DefaultClaudeModel)
		}
		if pinned {
			t.Fatalf("pinned = true for a global-backend daemon default; only an explicit task/PRD backend is a pin")
		}
	})

	t.Run("claude-code unit path: task-pinned claude-code backend, every tier empty, defaults to sonnet pinned", func(t *testing.T) {
		task := &Task{GuardrailBackend: claudeCodeBackendName}
		backend, model, pinned, err := resolveUnitGuardrailModelLocked(Config{VerificationBackend: "pi"}, task, nil)
		if err != nil {
			t.Fatalf("unit resolve = err %v; want the daemon default %q", err, DefaultClaudeModel)
		}
		if backend != claudeCodeBackendName || model != DefaultClaudeModel {
			t.Fatalf("got (%q, %q), want (%q, %q)", backend, model, claudeCodeBackendName, DefaultClaudeModel)
		}
		if !pinned {
			t.Fatalf("pinned = false for an explicitly task-pinned backend")
		}
	})

	t.Run("named premium claude-code model passes through unchanged (PRD pin)", func(t *testing.T) {
		const premium = "claude-opus-4-8"
		model, err := ResolveGuardrailModel(claudeCodeBackendName, premium, "", "", "", "")
		if err != nil {
			t.Fatalf("resolve = err %v; an operator-named model must pass through", err)
		}
		if model != premium {
			t.Fatalf("model = %q, want the operator-named %q (daemon default must not clobber it)", model, premium)
		}
	})

	t.Run("named premium claude-code model passes through unchanged (task pin)", func(t *testing.T) {
		const premium = "claude-opus-4-8"
		task := &Task{GuardrailModel: premium}
		backend, model, _, err := resolveUnitGuardrailModelLocked(Config{VerificationBackend: claudeCodeBackendName}, task, nil)
		if err != nil {
			t.Fatalf("unit resolve = err %v; an operator-named model must pass through", err)
		}
		if backend != claudeCodeBackendName || model != premium {
			t.Fatalf("got (%q, %q), want the operator-named (%q, %q) — the daemon default must not clobber it", backend, model, claudeCodeBackendName, premium)
		}
	})

	t.Run("named pin the claude-code backend cannot serve still fails loudly", func(t *testing.T) {
		model, err := ResolveGuardrailModel(claudeCodeBackendName, "ollama/qwen3.8:27b", "", "", "", "")
		if model != "" {
			t.Fatalf("model = %q; a named pin the backend cannot serve must not resolve to anything", model)
		}
		if !errors.Is(err, ErrNoGuardrailModel) {
			t.Fatalf("err = %v, want one wrapping %v — loud failure, never a default", err, ErrNoGuardrailModel)
		}
	})

	t.Run("pi-family judge with no model anywhere is untouched: loud failure, no default", func(t *testing.T) {
		model, err := ResolveGuardrailModel("pi", "", "", "", "", "")
		if model != "" {
			t.Fatalf("model = %q; a pi-family judge must never inherit a default", model)
		}
		if !errors.Is(err, ErrNoGuardrailModel) {
			t.Fatalf("err = %v, want one wrapping %v", err, ErrNoGuardrailModel)
		}
	})

	t.Run("pi-family judge with a named model is untouched: passes through", func(t *testing.T) {
		const pin = "ollama/qwen3.8:27b"
		model, err := ResolveGuardrailModel("pi", "", "", pin, "", "")
		if err != nil {
			t.Fatalf("resolve = err %v; a pairable pi pin must resolve", err)
		}
		if model != pin {
			t.Fatalf("model = %q, want the named pin %q", model, pin)
		}
	})
}

// TestClaudeExplicitModelPlanner pins the explicit-model policy
// (model_policy.go) at the planner: on a claude-code planning row with no
// model, the planner session gets the daemon default — sonnet — at the last
// tier of the resolution chain (resolvePlannedModel), never the CLI's
// account default; operator-named models at any tier pass through
// unchanged; a claude-code first row never falls through to the escalation
// row's model; and every non-claude backend keeps today's empty-model
// contract untouched.
func TestClaudeExplicitModelPlanner(t *testing.T) {
	const globalPin = "ollama/qwen3.8:27b"

	// resolvedPair is the per-session pair the planner chain produces,
	// mirroring decomposeStreamingCore: effectivePlanning picks the row
	// (first row wins, row's pair stays paired), then resolvePlannedModel
	// applies the tier chain — including the daemon-default tier for an
	// empty claude-code model. The PRD's own execution backend still counts
	// when no planning row named a backend.
	resolvedPair := func(auto *Automation, cfg Config, prdBackend, prdModel, prdDecomp string) (string, string) {
		planBackend, globalModel := effectivePlanning(auto, cfg)
		backend := planBackend
		if backend == "" && prdBackend != "" {
			// no planning row named a backend: the session runs on the PRD's
			backend = prdBackend
		}
		if backend == "" {
			return "", ""
		}
		return backend, resolvePlannedModel(backend, prdDecomp, prdBackend, prdModel, planBackend, globalModel, cfg.VerificationModel)
	}

	t.Run("first claude-code row, empty model: daemon default sonnet (not the escalation row's model)", func(t *testing.T) {
		auto := &Automation{PlanningBackend: claudeCodeBackendName}
		cfg := Config{PlanningBackend: "pi", PlanningModel: globalPin}
		backend, model := resolvedPair(auto, cfg, "", "", "")
		if backend != claudeCodeBackendName {
			t.Fatalf("backend = %q; the first row's backend must win, never the escalation row's", backend)
		}
		if model != DefaultClaudeModel {
			t.Fatalf("model = %q, want the daemon default %q — an empty first-row model must not fall through to the escalation row's %q", model, DefaultClaudeModel, globalPin)
		}
	})

	t.Run("global claude-code backend, empty model: daemon default sonnet", func(t *testing.T) {
		cfg := Config{PlanningBackend: claudeCodeBackendName}
		backend, model := resolvedPair(nil, cfg, "", "", "")
		if backend != claudeCodeBackendName || model != DefaultClaudeModel {
			t.Fatalf("got (%q, %q), want (%q, %q)", backend, model, claudeCodeBackendName, DefaultClaudeModel)
		}
	})

	t.Run("operator row naming an explicit claude-code model returns it unchanged", func(t *testing.T) {
		const named = "claude-opus-4-8"
		auto := &Automation{PlanningBackend: claudeCodeBackendName, PlanningModel: named}
		cfg := Config{PlanningBackend: claudeCodeBackendName, PlanningModel: "claude-haiku-4-5"}
		backend, model := resolvedPair(auto, cfg, "", "", "")
		if backend != claudeCodeBackendName || model != named {
			t.Fatalf("got (%q, %q), want the operator-named (%q, %q) — the daemon default must not clobber a named model", backend, model, claudeCodeBackendName, named)
		}
	})

	t.Run("PRD-named claude-code model beats the daemon default", func(t *testing.T) {
		const named = "claude-opus-5"
		cfg := Config{PlanningBackend: claudeCodeBackendName}
		backend, model := resolvedPair(nil, cfg, claudeCodeBackendName, named, "")
		if backend != claudeCodeBackendName || model != named {
			t.Fatalf("got (%q, %q), want (%q, %q) — an operator-named PRD model outranks the daemon default", backend, model, claudeCodeBackendName, named)
		}
	})

	t.Run("first row's named model beats the escalation row (order preserved)", func(t *testing.T) {
		const named = "claude-opus-4-8"
		auto := &Automation{PlanningBackend: claudeCodeBackendName, PlanningModel: named}
		cfg := Config{PlanningBackend: "pi", PlanningModel: globalPin}
		backend, model := resolvedPair(auto, cfg, "", "", "")
		if backend != claudeCodeBackendName || model != named {
			t.Fatalf("got (%q, %q), want the first row's (%q, %q)", backend, model, claudeCodeBackendName, named)
		}
	})

	t.Run("foreign global model does not reach a claude-code session: sonnet does", func(t *testing.T) {
		cfg := Config{PlanningBackend: "opencode", PlanningModel: globalPin}
		_, model := resolvedPair(nil, cfg, "", "", "")
		// backend here is the global row's "opencode" — the row itself is
		// foreign, so the daemon default must NOT bleed in for a non-claude
		// row; the session stays on the row's own (empty) model.
		if model != "" {
			t.Fatalf("model = %q on an opencode row, want empty — the daemon default is claude-code only", model)
		}
		_, model = resolvedPair(nil, Config{PlanningBackend: claudeCodeBackendName, PlanningModel: globalPin}, "", "", "")
		if model != DefaultClaudeModel {
			t.Fatalf("model = %q, want the daemon default %q — the foreign global must not reach a claude-code session", model, DefaultClaudeModel)
		}
	})

	t.Run("non-claude backends are untouched", func(t *testing.T) {
		cases := []struct {
			name      string
			auto      *Automation
			cfg       Config
			wantBack  string
			wantModel string
		}{
			{"global pi row, empty model stays empty for the caller to fail loudly", nil, Config{PlanningBackend: "pi"}, "pi", ""},
			{"global pi row, named model passes through", nil, Config{PlanningBackend: "pi", PlanningModel: globalPin}, "pi", globalPin},
			{"pi first row, empty model stays empty even with a named escalation row", &Automation{PlanningBackend: "pi"}, Config{PlanningBackend: "pi", PlanningModel: globalPin}, "pi", ""},
			{"opencode row stays empty", nil, Config{PlanningBackend: "opencode"}, "opencode", ""},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				backend, model := resolvedPair(c.auto, c.cfg, "", "", "")
				if backend != c.wantBack || model != c.wantModel {
					t.Fatalf("got (%q, %q), want (%q, %q)", backend, model, c.wantBack, c.wantModel)
				}
			})
		}
	})
}
