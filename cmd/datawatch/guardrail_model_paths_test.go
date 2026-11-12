package main

import (
	"errors"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// prdShapePinnedModel is the exact pinned model of the PRD shape
// observed on 2026-10-20 22:29: PRD backend is pi, no guardrail model at
// any unit level, and both autonomous.verification_model and
// autonomous.planning_model are "ollama/qwen3.8-64k:27b". A pi-family
// judge/verify/decompose launch must name exactly this model — never
// truncated to "ollama/qwen3.8:27b", never substituted with the
// harness's host-default model ("pi default"), and never dropped.
const prdShapePinnedModel = "ollama/qwen3.8-64k:27b"

const truncatedLabel = "ollama/qwen3.8:27b"

// TestJudgeKeepsPinnedModel_PRDShape is the daemon-side regression test
// for the 2026-10-20 22:29 shape. Every pi-family session launch the
// daemon routes through a judge or a verify — per-task guardrail,
// per-story guardrail, rules-review, ci-watch, rejudge (all of them the
// one-shot judge session, whose model guard is guardPiFamilySessionModel)
// and the autonomous verify session (guardPiFamilyVerifyModel) — must
// keep the pinned model intact.
func TestJudgeKeepsPinnedModel_PRDShape(t *testing.T) {
	// One-shot judge session: the manager pre-resolves the unit's model
	// (resolveUnitGuardrailModel) and the daemon enforces the pin here.
	model, verdict := guardPiFamilySessionModel(
		"pi", "pi", "rules-review", prdShapePinnedModel,
		prdShapePinnedModel, prdShapePinnedModel, prdShapePinnedModel,
		"ollama/qwen3:32b", // a non-pi host default that must never leak in
	)
	if verdict != nil {
		t.Fatalf("pinned PRD shape produced a failure verdict: %+v", verdict)
	}
	assertExactModel(t, "guardPiFamilySessionModel (rules-review)", model)

	// Verify session: same model must come through, no setup block.
	vmodel, setup := guardPiFamilyVerifyModel("pi", prdShapePinnedModel, prdShapePinnedModel)
	if setup != nil {
		t.Fatalf("pinned PRD shape produced a setup verdict: %+v", setup)
	}
	assertExactModel(t, "guardPiFamilyVerifyModel", vmodel)

	// Decompose session gate (manager side): the pinned planning model
	// passes the launch gate.
	if serr := autonomouspkg.RequirePinnedModel("pi", prdShapePinnedModel,
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); serr != nil {
		t.Fatalf("pinned decompose model rejected by the launch gate: %v", serr)
	}

	// And the shape that produced the incident: every tier empty must
	// fail loudly on every one of these paths, never launch with the
	// host default.
	if _, v := guardPiFamilySessionModel("pi", "pi", "rules-review", "", "", "", "", "ollama/qwen3:32b"); v == nil {
		t.Fatal("guardPiFamilySessionModel with no tier: want a loud fail verdict, got a launch")
	}
	if _, v := guardPiFamilySessionModel("pi-prompt", "pi", "rules-review", "", "", "", "", "ollama/qwen3:32b"); v == nil {
		t.Fatal("guardPiFamilySessionModel (pi-prompt) with no tier: want a loud fail verdict, got a launch")
	}
	if _, s := guardPiFamilyVerifyModel("pi", "", ""); s == nil {
		t.Fatal("guardPiFamilyVerifyModel with no tier: want a loud setup verdict, got a launch")
	}
	if serr := autonomouspkg.RequirePinnedModel("pi", "",
		"the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); serr == nil {
		t.Fatal("decompose launch gate with empty model: want an ErrNoPinnedModel error, got nil")
	} else if !errors.Is(serr, autonomouspkg.ErrNoPinnedModel) {
		t.Fatalf("decompose gate error = %v, want one wrapping ErrNoPinnedModel", serr)
	}
}

func assertExactModel(t *testing.T, where, got string) {
	t.Helper()
	if got != prdShapePinnedModel {
		t.Fatalf("%s returned model %q, want exactly %q", where, got, prdShapePinnedModel)
	}
	if got == truncatedLabel {
		t.Fatalf("%s returned the truncated model %q", where, got)
	}
	if got == "" || got == "pi default" {
		t.Fatalf("%s returned %q (host default), want the pinned model", where, got)
	}
}

// TestGuardPiFamilySessionModel_PathTierTable covers every one-shot
// judge launch path the daemon owns (per-task guardrail, per-story
// guardrail, rules-review, ci-watch, rejudge — all funnel through this
// guard) against every precedence tier: the invocation's pre-resolved
// unit model first, then autonomous.guardrail_model →
// autonomous.verification_model → autonomous.planning_model.
func TestGuardPiFamilySessionModel_PathTierTable(t *testing.T) {
	paths := []string{"per-task", "per-story", "rules-review", "ci-watch", "rejudge"}
	cfg := map[string]struct {
		cfgGuardrail, cfgVerification, cfgPlanning string
	}{
		"autonomous.guardrail_model tier":      {"g/guard:27b", "g/ver:27b", "g/plan:27b"},
		"autonomous.verification_model tier":   {"", "g/ver:27b", "g/plan:27b"},
		"autonomous.planning_model tier":       {"", "", "g/plan:27b"},
	}
	for _, path := range paths {
		for tierName, tiers := range cfg {
			t.Run(path+" + "+tierName, func(t *testing.T) {
				want := tiers.cfgGuardrail
				if want == "" {
					want = tiers.cfgVerification
				}
				if want == "" {
					want = tiers.cfgPlanning
				}
				model, verdict := guardPiFamilySessionModel(
					"pi", "pi", path, "", // unit model unset → the tier resolves it
					tiers.cfgGuardrail, tiers.cfgVerification, tiers.cfgPlanning,
					"claude-opus-4-1-2025-08-05", // non-pi default: never allowed on pi-family
				)
				if verdict != nil {
					t.Fatalf("%s: unexpected verdict %+v", path, verdict)
				}
				if model != want {
					t.Fatalf("%s: model = %q, want exactly %q", path, model, want)
				}
				if model == "claude-opus-4-1-2025-08-05" {
					t.Fatalf("%s: a non-pi registry default leaked into the pi judge session", path)
				}
			})
		}
	}

	// The unit (invocation pre-resolved) model always wins over global tiers.
	for _, path := range paths {
		t.Run(path+" + unit pre-resolved model wins", func(t *testing.T) {
			model, verdict := guardPiFamilySessionModel(
				"pi", "pi", path, "unit/pinned:27b",
				"g/guard:27b", "g/ver:27b", "g/plan:27b", "x/y",
			)
			if verdict != nil || model != "unit/pinned:27b" {
				t.Fatalf("%s: got model %q verdict %+v, want exactly unit/pinned:27b", path, model, verdict)
			}
		})
	}

	// pi-prompt is the same family, same discipline.
	model, verdict := guardPiFamilySessionModel("pi-prompt", "pi", "rules-review", "", "", "pp/ver:27b", "", "")
	if verdict != nil || model != "pp/ver:27b" {
		t.Fatalf("pi-prompt judge session: got model %q verdict %+v, want pp/ver:27b", model, verdict)
	}

	// Non-pi kinds keep their existing fallback behavior.
	for _, kind := range []string{"claude-code", "codex", "codex-prompt"} {
		t.Run("non-pi kind "+kind, func(t *testing.T) {
			if m, v := guardPiFamilySessionModel(kind, kind, "rules-review", "unit/x", "a/b", "c/d", "e/f", "registry/default"); v != nil || m != "unit/x" {
				t.Fatalf("override: got model %q verdict %+v, want unit/x", m, v)
			}
			if m, v := guardPiFamilySessionModel(kind, kind, "rules-review", "", "a/b", "c/d", "e/f", "registry/default"); v != nil || m != "registry/default" {
				t.Fatalf("fallback: got model %q verdict %+v, want registry/default", m, v)
			}
		})
	}
}

// TestGuardPiFamilySessionModel_FailLoudWithoutTier asserts the
// fail-loudly contract: with no tier resolving a servable model, no
// judge path launches a session on the host default; the guard returns
// a loud, non-retried infra block that records the configuration
// failure instead of a finding about the work.
func TestGuardPiFamilySessionModel_FailLoudWithoutTier(t *testing.T) {
	for _, kind := range []string{"pi", "pi-prompt"} {
		model, verdict := guardPiFamilySessionModel(kind, "pi", "rules-review", "", "", "", "", "ollama/qwen3.8:27b")
		if model != "" {
			t.Fatalf("[%s] model %q resolved with no tier set; must be empty", kind, model)
		}
		if verdict == nil {
			t.Fatalf("[%s] want a loud verdict, got nil (a launch would run on the host default)", kind)
		}
		if verdict.Outcome != "block" || verdict.Category != "infra" {
			t.Fatalf("[%s] verdict = %+v, want a loud infra block", kind, verdict)
		}
		s := verdict.Summary + strings.Join(verdict.Issues, " ")
		for _, key := range []string{"autonomous.guardrail_model", "autonomous.verification_model", "autonomous.planning_model"} {
			if !strings.Contains(s, key) {
				t.Fatalf("[%s] loud verdict does not name configuration key %q: %+v", kind, key, verdict)
			}
		}
		if strings.Contains(s, "ollama/qwen3.8:27b") {
			t.Fatalf("[%s] loud verdict mentioned the truncated host-default label %q", kind, truncatedLabel)
		}
	}
}

// TestGuardPiFamilyVerifyModel_TiersAndFailLoud pins the autonomous
// verify session's model guard: autonomous.verification_model first,
// autonomous.planning_model second, and a loud "setup" block when neither
// is set — never a launch on the pi host default.
func TestGuardPiFamilyVerifyModel_TiersAndFailLoud(t *testing.T) {
	if m, v := guardPiFamilyVerifyModel("pi", "ver/tier:27b", "plan/tier:27b"); v != nil || m != "ver/tier:27b" {
		t.Fatalf("verification tier: got model %q setup %+v, want ver/tier:27b", m, v)
	}
	if m, v := guardPiFamilyVerifyModel("pi", "", "plan/tier:27b"); v != nil || m != "plan/tier:27b" {
		t.Fatalf("planning tier: got model %q setup %+v, want plan/tier:27b", m, v)
	}
	if m, v := guardPiFamilyVerifyModel("pi", "claude-haiku-5-5", "plan/tier:27b"); v != nil || m != "plan/tier:27b" {
		t.Fatalf("unservable verification tier must be skipped: got model %q setup %+v, want plan/tier:27b", m, v)
	}
	m, setup := guardPiFamilyVerifyModel("pi", "", "")
	if m != "" {
		t.Fatalf("model %q resolved with no tier set; must be empty", m)
	}
	if setup == nil {
		t.Fatal("want a loud setup verdict, got nil (a launch would run on the host default)")
	}
	if setup.OK || setup.Category != "setup" {
		t.Fatalf("setup verdict = %+v, want a loud setup", setup)
	}
	if !strings.Contains(setup.Summary, "no pinned model") {
		t.Fatalf("setup summary %q must name the missing pinned model", setup.Summary)
	}
}
