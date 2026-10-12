// B124 — autonomousVerify and autonomousGuardrail live inside main() and
// are not unit-testable directly (same constraint bl369_prompt_security_test.go
// documents); source inspection confirms the session-routing branch this
// fix added is actually wired in, not just defined and unused.
package main

import "testing"

func TestB124_AutonomousVerify_RoutesThroughSessionWhenNeeded(t *testing.T) {
	src := readMainGo(t)
	checks := []struct {
		name string
		want string
	}{
		{"resolves useSession", "vbackend, vkind, verifyModel, useSession = resolveVerifierBackendModel("},
		{"passes project dir + session-kind check", "prd.Backend, prd.Model, prd.ProjectDir, resolveAskBackend, askCompatible, isInferenceSessionKind)"},
		{"branches on useSession", "if useSession {"},
		{"spawns a real session", "runOneShotJSONSession(cfg, prd.ID, prd.ProjectDir, vbackend, verifyModel,"},
		{"reuses the same judgment prompt", "sessionJudgmentTask(prompt, prd.ProjectDir, outputFile)"},
	}
	for _, c := range checks {
		if !contains(src, c.want) {
			t.Errorf("autonomousVerify: missing %q in main.go", c.name)
		}
	}
}

func TestB124_AutonomousGuardrail_RoutesThroughSessionWhenNeeded(t *testing.T) {
	src := readMainGo(t)
	checks := []struct {
		name string
		want string
	}{
		{"checks needsSessionJudgment before downgrading", "if needsSessionJudgment(gkind, req.ProjectDir, isInferenceSessionKind) {"},
		{"spawns a real session", "runOneShotJSONSession(cfg, req.PRDID, req.ProjectDir, gbackend, guardrailModel,"},
		{"reuses the same judgment prompt", "sessionJudgmentTask(prompt, req.ProjectDir, outputFile)"},
		// The old unconditional downgrade must still exist as the
		// no-project-dir fallback, just gated behind the session check
		// now rather than applying unconditionally.
		{"still has the ollama fallback for the no-project-dir case", `gkind = "ollama"`},
	}
	for _, c := range checks {
		if !contains(src, c.want) {
			t.Errorf("autonomousGuardrail: missing %q in main.go", c.name)
		}
	}
}
