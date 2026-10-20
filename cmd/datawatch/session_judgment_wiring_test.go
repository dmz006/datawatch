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
		// Selection (incl. resolveVerifierBackendModel's useSession
		// decision) lives in selectVerifierLLM (verification_llm.go).
		{"resolves useSession", "vbackend, vkind, verifyModel, useSession := vsel.Backend, vsel.Kind, vsel.Model, vsel.UseSession"},
		{"passes project dir + session-kind check", "ProjectDir:     taskDir,"},
		{"passes session-kind check", "verifierTryAdmit, resolveAskBackend, askCompatible, isInferenceSessionKind)"},
		{"branches on useSession", "if useSession {"},
		{"spawns a real session", "runOneShotJSONSessionWithEffort(cfg, prd.ID, taskDir, vbackend, verifyModel,"},
		{"reuses the same judgment prompt", "sessionJudgmentTask(prompt, taskDir, outputFile)"},
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
		{"spawns a real session and captures its id", "runOneShotJudgeSessionCaptureID(cfg, req.PRDID, req.ProjectDir, gbackend, sessionModel,"},
		{"stamps the judge session onto its verdicts", "judgeVerdictWithSession(autonomouspkg.GuardrailVerdict{"},
		{"reuses the same judgment prompt", "sessionJudgmentTask(prompt, req.ProjectDir, outputFile)"},
		// The old unconditional downgrade must still exist as the
		// no-project-dir fallback, just gated behind the session check
		// now rather than applying unconditionally.
		{"still has the ollama fallback for the no-project-dir case", `gkind = "ollama"`},
		// B124 follow-up (v9.0.45) — the session path must never carry
		// the flat ask-compatible VerificationModel default through to
		// a session call; only a genuine per-unit override may.
		{"session model comes from the per-unit override, not the merged ask-model fallback", "sessionModel := req.GuardrailModel"},
	}
	for _, c := range checks {
		if !contains(src, c.want) {
			t.Errorf("autonomousGuardrail: missing %q in main.go", c.name)
		}
	}
}
